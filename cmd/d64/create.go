package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/juster/c64/disk"
)

const (
	defaultDiskId = "0000"
)

var (
	createFlags flag.FlagSet
	labelFlag   = createFlags.String("lab", "", "disk label for the d64 image")
	newFileFlag = createFlags.String("f", "", "path to d64 file to create")
	diskIdFlag  = createFlags.String("id", "", "disk ID (two bytes) in hexadecimal")
)

func createUsage() {
	fmt.Fprintf(createFlags.Output(), "usage: %s create <-f dest.d64> <-lab \"disk label\"> [-id 010F] <file1> <file2...>\n", self)
	createFlags.PrintDefaults()
	os.Exit(2)
}

func create(args []string) int {
	createFlags.Usage = createUsage
	createFlags.Init("create", flag.ExitOnError)
	createFlags.Parse(args)
	inputs := createFlags.Args()

	if len(inputs) == 0 {
		createUsage()
	}
	if *newFileFlag == "" {
		log.Print("error: -f is required to provide the new file name")
		createUsage()
	}
	if *labelFlag == "" {
		log.Print("error: -lab is require to specify the disk label")
		createUsage()
	}
	var diskId []byte
	if *diskIdFlag == "" {
		*diskIdFlag = defaultDiskId
	}
	diskId, err := hex.DecodeString(*diskIdFlag)
	if err != nil {
		log.Fatal("error: bad -id flag: %v", err)
	}
	if len(diskId) != 2 {
		log.Fatal("error: bad -id flag: must be hexadecimal for two bytes (4 chars)")
	}

	log.SetPrefix("create: ")

	if len(inputs) > 1 {
		log.Fatal("TODO: create is only implemented for a single file")
	}

	var bad bool
	for _, path := range inputs {
		_, err := os.Stat(path)
		if err != nil {
			log.Print(err)
			bad = true
		}
	}
	if bad {
		return 1
	}

	var d disk.Img
	f, err := os.Create(*newFileFlag)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	path := inputs[0]
	buf, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	fname := basename(path)
	fname = strings.ToUpper(fname)
	d.Init(fname, "\x00\x00")

	createFile(fname, buf, &d)
	if _, err = f.Write(d[:]); err != nil {
		log.Fatal(err)
	}
	return 0
}

func basename(path string) string {
	fname := filepath.Base(path)
	if i := strings.LastIndex(fname, "."); i >= 0 {
		fname = fname[:i]
	}
	if len(fname) == 0 {
		log.Fatalf("invalid filename: %s", fname)
	}
	return fname
}

func createFile(fname string, buf []byte, d *disk.Img) {
	fname = strings.ToUpper(fname)

	bam := d.BAM()

	a := bam.NewAllocator()
	ts := a.Alloc()
	if ts.T == 0 {
		log.Fatal("allocation failed")
	}
	ent, err := d.NewDirEntry()
	if err != nil {
		log.Fatal(err)
	}
	ent.SetFilename(fname)
	ent.SetByteLen(len(buf))
	ent.FileType = disk.PRG
	ent.FileTS = ts

	if err = writeProgram(d, ts, a, buf); err != nil {
		log.Fatal(err)
	}
}

func writeProgram(d *disk.Img, ts disk.TS, a *disk.Allocator, buf []byte) error {
	prg := (*disk.PrgBlock)(d.Block(ts))
	prg.SetLoadAddr(buf[:2])
	buf = buf[2:]
	if len(buf) <= 252 {
		prg.Link.T = 0
		prg.Link.S = uint8(copy(prg.Data[:], buf))
		return nil
	}
	copy(prg.Data[:], buf[:252])
	blk := (*disk.RawBlock)(d.Block(ts))
	return writeRawBlocks(d, blk, a, buf[252:])
}

func writeRawBlocks(d *disk.Img, blk *disk.RawBlock, a *disk.Allocator, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	var i int
	var ts disk.TS
	for {
		// take the next block from the BAM and link the current block to it
		ts = a.Alloc()
		if ts.T == 0 {
			return errors.New("disk full")
		}
		blk.Link = ts

		if len(buf)-i <= 254 {
			// write the last block when there are at most 254 bytes left
			break
		}
		// there are at least 2 more blocks to write
		blk = (*disk.RawBlock)(d.Block(ts))
		i += copy(blk.Data[:], buf[i:i+254])
	}

	tail := (*disk.RawBlock)(d.Block(ts))
	tail.EndFile(uint8(copy(tail.Data[:], buf[i:])))
	return nil
}
