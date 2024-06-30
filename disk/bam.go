package disk

import (
	"errors"
)

var (
	OutOfRange = errors.New("out of BAM range")
	BAMConflict = errors.New("already taken or freed")
	DiskFull = errors.New("disk full")
)

type BAMEntry struct {
	Count byte;
	free [3]byte;
}

type BAM struct {
	DirTS TS;
	DriveFormat byte;
	Unused1 byte;
	AvailMap [totalTrackCount]BAMEntry;
	DiskName [16]byte;
	// third byte is always padding
	DiskID [3]byte;
	DOSVersion [6]byte;
	Unused2 [86]byte
}

// Init initializes the BAM to mark all sectors as free.

func (bam *BAM) Init(name, id string) error {
	if len(name) > 16 {
		return errors.New("name overflow")
	}
	if len(id) != 2 {
		return errors.New("invalid disk id")
	}
	var j int
	bam.DriveFormat = bamDriveFormat1541
	for i := range bam.AvailMap {
		if uint8(i + 1) > geometry[j].trackMax {
			j++
		}
		bam.AvailMap[i].Count = geometry[j].sectorCount
		for k := range bam.AvailMap[i].free {
			bam.AvailMap[i].free[k] = 0xFF
		}
	}
	copy(bam.DiskName[:], PadString(name, 16))
	copy(bam.DiskID[:], PadString(id, 3))
	copy(bam.DOSVersion[:], PadString(bamDOSVersion, 6))
	return nil
}

func (bam *BAM) Entry(ts TS) *BAMEntry {
	if int(ts.T) >= len(bam.AvailMap) {
		return nil
	}
	if ts.S >= 32 {
		return nil
	}
	return &bam.AvailMap[ts.T - 1]
}

// Alloc marks a block as taken and unavailable for use. Returns an error if the available
// block count is already 0 or if the same block was previously taken.
func (bam *BAM) Alloc(ts TS) error {
	ent := bam.Entry(ts)
	if ent == nil {
		return OutOfRange
	}
	if ent.Count == 0 {
		return errors.New("available count already 0")
	}
	i, j := ts.S / 8, byte(1 << (ts.S % 8))
	x := ent.free[i]
	if x & j == 0 {
		return BAMConflict
	}
	ent.free[i] = x ^ j
	ent.Count--
	return nil
}

// Free mark a block as available for use. Returns an error if the block was
// already available.
func (bam *BAM) Free(ts TS) error {
	ent := bam.Entry(ts)
	if ent == nil {
		return OutOfRange
	}
	i, j := ts.S / 8, byte(1 << (ts.S % 8))
	x := ent.free[i]
	if x & j > 0 {
		return BAMConflict
	}
	ent.free[i] = x | j
	ent.Count++
	return nil
}

// Avail checks if a track/sector is available or if it has already been marked
// as allocated in the BAM.
func (bam *BAM) Avail(ts TS) bool {
	ent := bam.Entry(ts)
	if ent == nil {
		panic(OutOfRange)
	}
	i, j := ts.S / 8, byte(1 << (ts.S % 8))
	return ent.free[i] & j > 0
}

type NextTrackFunc = func (uint8) uint8

type Allocator struct {
	bam *BAM
	// Lookahead for the next track/sector to attempt to allocate.
	TS TS
	// There are gaps of sectors between allocated blocks because it is easier
	// to read on the spinning disk than blocks that are allocated
	// contiguously.
	SectorStagger uint8
	// Next available track algorithm may be overridden.
	NextTrack NextTrackFunc
}

// The defaultNextTrack function looks for tracks outside of the BAM (middle)
// track. After those run it it looks for tracks from the BAM track inwards.
func defaultNextTrack(prev uint8) uint8 {
	var next uint8
	if prev > bamTrack {
		next = prev + 1
		if next <= totalTrackCount {
			return next
		}
		prev = bamTrack
	}
	next = prev - 1
	if next > 0 {
		return next
	}
	return 0
}

func (bam *BAM) NewAllocator() *Allocator {
	return &Allocator{
		bam: bam,
		// Start trying to allocate at the track directly after the BAM.
		TS: TS{bamTrack + 1, 0},
		SectorStagger: SectorFileStagger,
		NextTrack: defaultNextTrack,
	}
}

func (a *Allocator) Alloc() (TS, error) {
	if a.TS.T == 0 {
		return TS{0, 0}, DiskFull
	}

	ts := a.TS
	if !a.bam.Avail(ts) {
		ts = a.nextTS(ts)
		if ts.T == 0 {
			a.TS = ts
			return ts, nil
		}
	}

	if err := a.bam.Alloc(ts); err != nil {
		return TS{0, 0}, err
	}
	// Lookahead to the next track/sector to attempt to alloc.
	a.TS = a.nextTS(ts)
	return ts, nil
}

func (a *Allocator) nextTS(ts TS) TS {
	var next TS
	for iter := ts; iter.T > 0; iter = next {
		next = a.nextAvailBlock(iter)
		if next.T > 0 {
			break
		}
		next.T, next.S = a.NextTrack(iter.T), 0
		if next.T == 0 {
			return TS{}
		}
	
	}
	return next
}

// nextAvailBlock finds the next available block in the same track as ts, starting
// with ts. Returns TS{0, 0} if no blocks are available on that track.
func (a *Allocator) nextAvailBlock(ts TS) TS {
	max := sectorCount(ts.T) - 1

	// Check every sector on the track.
	for i := uint8(0); i <= max; i++ {
		if a.bam.Avail(ts) {
			return ts
		}
		ts.S += a.SectorStagger
		if ts.S > max {
			ts.S = 1 + ts.S % max
		}
	}
	
	return TS{}
}
