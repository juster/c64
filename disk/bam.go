package disk

import (
	"errors"
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
