package disk

// Derived almost exclusively from "Inside Commodore DOS"
// https://ia800405.us.archive.org/19/items/Inside_Commodore_Dos/Inside_Commodore_Dos.pdf

import (
	"errors"
	"unsafe"
)

const (
	blockSize = 256
	totalTrackCount = 35
	totalBlockCount = 683
	totalByteCount = 174848
)

const (
	bamTrack = 18
	bamDriveFormat1541 = 'A' // "1541 and 4040"
	bamDOSVersion = "\x32\x41"
	SectorFileStagger = 10
	SectorDirStagger = 3
)

// File type bytes (for reference).
const (
	Scratched = 0x00
	DEL = 0x80
	SEQ = 0x81
	PRG = 0x82
	USR = 0x83
	REL = 0x84
	UnclosedDEL = 0x00
	UnclosedSEQ = 0x01
	UnclosedPRG = 0x02
	UnclosedUSR = 0x03
	UnclosedREL = 0x04 // "cannot occur"
	ReplaceDEL = 0xA0
	ReplaceSEQ = 0xA1
	ReplacePRG = 0xA2
	ReplaceUSR = 0xA3
	ReplaceREL = 0xA4
	LockDEL = 0xC2
	LockSEQ = 0xC3
	LockPRG = 0xC4
	LockUSR = 0xC5
	LockREL = 0xC6
)

var (
	BadTS = errors.New("invalid track/sector")
)

type geom struct {
	trackMin, trackMax, sectorCount uint8
	sectorOffset uint16
}

type geometryTable [4]geom

var geometry = geometryTable{
	{1, 17, 21, 0},
	{18, 24, 19, 357},
	{25, 30, 18, 490},
	{31, 40, 17, 598},
	// the average disk has 35 tracks and 683 sectors/blocks
	// special disks later added tracks for 40 total
}

func (tbl *geometryTable) Lookup(track uint8) (geom, error) {
	for _, g := range tbl {
		if g.trackMin <= track && track <= g.trackMax {
			return g, nil
		}
	}
	return geom{}, BadTS
}

func sectorCount(track uint8) uint8 {
	g, err := geometry.Lookup(track)
	if err != nil {
		panic(err)
	}
	return g.sectorCount
}

func trackCapacity(track uint8) uint16 {
	return blockSize * uint16(sectorCount(track))
}

type TS struct {
	// Tracks are 1-indexed and sectors are 0-indexed.
    T, S uint8
}

func (ts TS) Offset() (uint32, error) {
	if ts.T < 1 {
		return 0, BadTS
	}
	g, err := geometry.Lookup(ts.T)
	if err != nil {
		return 0, BadTS
	}
	if g.sectorCount <= ts.S {
		// sector exceeded the maximum
		return 0, BadTS
	}
	sectors := uint32(ts.T - g.trackMin)
	sectors *= uint32(g.sectorCount)
	sectors += uint32(ts.S)
	return blockSize * (uint32(g.sectorOffset) + sectors), nil
}

func (ts TS) IsValid() bool {
	g, err := geometry.Lookup(ts.T)
	if err != nil {
		return false
	}
	return ts.S < g.sectorCount
}

func (ts *TS) IsNull() bool {
	return ts.T == 0
}

// FileBlock provides an interface for iterating through the blocks of the file and reading the data.
type FileBlock interface {
	NextBlock(*Img) FileBlock
	Bytes() []byte
	Len() uint8
}

type DirBlock struct {
	Files [8]DirEntry;
}

func (dir *DirBlock) Init() {
	dir.Files[0].DirLink = TS{0, 255}
}

func (dir *DirBlock) NextAvail() *DirEntry {
	for i := range dir.Files {
		if dir.Files[i].FileType == Scratched {
			return &dir.Files[i]
		}
	}
	return nil
}

func (dir *DirBlock) Next() (TS, bool) {
	ts := dir.Files[0].DirLink
	if ts.T == 0 && ts.S == 255 {
		return ts, false
	}
	return ts, true
}

func (dir *DirBlock) SetNext(ts TS) {
	dir.Files[0].DirLink = ts
}

type DirEntry struct {
	// Only the DirLink in the first file entry of the directory block is set to a value
	// The others are zeroed.
	DirLink TS;
	FileType uint8;
	FileTS TS;
	Filename [16]byte;
	// only for REL files
	RelSideSector TS;
	RelRecordSize byte;
	Unused [4]byte;
	// used internally by the DOS
	SaveReplace TS
	// number of blocks used in little endian 16-bit
	BlockSizeLo uint8;
	BlockSizeHi uint8
}

func (fe *DirEntry) IsScratched() bool {
	return fe.FileType == Scratched
}

func (fe *DirEntry) FilenameString() string {
	return UnpadBytes(fe.Filename[:])
}

func (fe *DirEntry) SetFilename(filename string) {
	copy(fe.Filename[:], PadString(filename, 16))
}

func (fe *DirEntry) BlockCount() uint16 {
	return uint16(fe.BlockSizeLo) | uint16(fe.BlockSizeHi) << 8
}

// SetBlockCount sets the 16-bit size of a file, in blocks, in little-endian format.
func (fe *DirEntry) SetBlockCount(bcount uint16) {
	fe.BlockSizeLo = uint8(bcount & 255)
	fe.BlockSizeHi = uint8(bcount >> 8) & 255
}

// SetByteLen converts from byte size to block size and then stores this in the
// file entry.
func (fe *DirEntry) SetByteLen(size int) {
	n := size / blockSize
	if size % blockSize > 0 {
		n++
	}
	fe.SetBlockCount(uint16(n))
}

func (fe *DirEntry) FileBlock(d *Img) FileBlock {
	raw := d.Block(fe.FileTS)
	switch fe.FileType {
	case DEL, SEQ: return (*RawBlock)(raw)
	case PRG: return (*PrgBlock)(raw)
	case USR, REL: panic("unimplemented")
	default: panic("unknown file type")
	}
}

// PRG files have a PrgBlock, followed by RawBlocks.
// SEQ files have RawBlocks.

type RawBlock struct {
	Link TS;
	Data [254]byte;
}

// EOF checks if this is the last block in the file.
func (fb *RawBlock) EOF() bool {
	return fb.Link.T == 0
}

func (fb *RawBlock) EndFile(size uint8) {
	fb.Link = TS{0, size}
}

// Truncate sets this RawBlock as the last block in the file and stores the data at the same time.
func (fb *RawBlock) Truncate(end []byte) error {
	if len(end) > 254 {
		return errors.New("overflow")
	}
	fb.Link = TS{0, uint8(len(end))}
	copy(fb.Data[:], end)
	return nil
}

// NextBlock returns nil if this is the last block in the file or reads the next RawBlock.
func (fb *RawBlock) NextBlock(d *Img) FileBlock {
	if fb.EOF() {
		return nil
	}
	return (*RawBlock)(d.Block(fb.Link))
}

func (fb *RawBlock) Len() uint8 {
	if fb.EOF() {
		return fb.Link.S
	}
	return 254
}

func (fb *RawBlock) Bytes() []byte {
	if fb.EOF() {
		return fb.Data[:fb.Len()]
	}
	return fb.Data[:]
}

type PrgBlock struct {
	Link TS;
	LoadLo uint8;
	LoadHi uint8;
	Data [252]byte;
}

func (prg *PrgBlock) SetLoadAddr(addr []byte) {
	if len(addr) != 2 {
		panic("invalid 16-bit address")
	}
	prg.LoadLo = addr[0]
	prg.LoadHi = addr[1]
}

func (prg *PrgBlock) SetC64Load() {
	prg.SetLoadAddr([]byte("\x08\x01"))
}

func (prg *PrgBlock) NextBlock(d *Img) FileBlock {
	if prg.Link.T == 0 {
		return nil
	}
	return (*RawBlock)(d.Block(prg.Link))
}

// Len includes the load address in the length.
func (prg *PrgBlock) Len() uint8 {
	return 254
}

// Bytes returns the two-byte load address before the beginning of the PRG data. This is often
// stored in the file itself when writing the PRG to an external file on another file system.
func (prg *PrgBlock) Bytes() []byte {
	// Uses the RawBlock type which also skips the track/sector
	return (*RawBlock)(unsafe.Pointer(prg)).Bytes()
}

type Img [totalByteCount]byte;

func (d *Img) Init(name, id string) error {
	dirts := TS{bamTrack, 1}
	bam := d.BAM()
	if err := bam.Init(name, id); err != nil {
		return err
	}
	bam.DirTS = dirts
	bam.Alloc(TS{bamTrack, 0})
	bam.Alloc(TS{bamTrack, 1})
	dir := (*DirBlock)(d.Block(dirts))
	dir.Init()

	return nil
}

func (d *Img) BAM() *BAM {
	return (*BAM)(d.Block(TS{bamTrack, 0}))
}

func (d *Img) Dir() *DirBlock {
	bam := d.BAM()
	return (*DirBlock)(d.Block(bam.DirTS))
}

func (d *Img) Block(ts TS) unsafe.Pointer {
	off, err := ts.Offset()
	if err != nil {
		panic(err)
	}
	if off + blockSize > totalByteCount {
		// double-check if there is a bug in ts.Offset()
		panic("overflow")
	}
	return unsafe.Add(unsafe.Pointer(d), off)
}

func (d *Img) NewDirEntry() (*DirEntry, error) {
	bam := d.BAM()
	ts := bam.DirTS
	dir := (*DirBlock)(d.Block(ts))
	a := bam.NewAllocator()
	a.TS = bam.DirTS
	a.SectorStagger = SectorDirStagger
	a.NextTrack = func (_ uint8) uint8 { return 0 }

	// find the next available dir entry
	var file *DirEntry
	for {
		if file = dir.NextAvail(); file != nil {
			break
		}
		// skip to next directory block, if it already exists
		if next, ok := dir.Next(); ok {
			dir = (*DirBlock)(d.Block(next))
			continue
		}
		// try to allocate the next directory block
		ts, err := a.Alloc()
		switch err {
		case nil:
			// do nothing
		case DiskFull:
			return nil, errors.New("no room left in directory track")
		default:
			return nil, err
		}
		dir.SetNext(ts)
		dir = (*DirBlock)(d.Block(ts))
	}
	return file, nil
}

