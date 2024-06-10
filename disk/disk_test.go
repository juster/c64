package disk

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"testing"
)

func TestDiskBlock(t *testing.T) {
	var d Img
	dir := (*DirBlock)(d.Block(TS{18, 1}))
	first := &dir.Files[0]
	first.DirLink = TS{0, 0xFF}
	first.FileType = PRG

	off, _ := TS{18, 1}.Offset()
	if bytes.Compare(d[off:off+3], []byte{0, 0xFF, 0x82}) != 0 {
		t.Fatal("storing to structure pointer failed")
	}

	off1, _ := (TS{18,0}).Offset()
	off2, _ := (TS{18,1}).Offset()
	if off2 - off1 != blockSize {
		t.Error("offset should increment by blocksize in simply case")
	}

	defer func() {
		if recover() != BadTS {
			t.Fatal("incorrect error")
		}
	}()
	d.Block(TS{101,202})
	t.Fatal("should have errored")
}

func TestBAM(t *testing.T) {
	var d Img
	d.Init("TESTNAME", "\x01\x02")
	bam := d.BAM()
	bam.Alloc(TS{1, 0})
	bam.Alloc(TS{1, 1})
	bam.Alloc(TS{1, 8})

	off, _ := TS{18, 0}.Offset()
	// Track 18 has 21 total sectors but only the available map for track 1 is checked.
	if bytes.Compare(d[off:off+8], []byte{18, 1, 'A', 0, 18, 0xFC, 0xFE, 0xFF}) != 0 {
		t.Error("failed to init BAM:", bam)
	}

	nameoff := off+4+4*35
	if bytes.Compare(d[nameoff:nameoff+16], PadString("TESTNAME", 16)) != 0 {
		t.Error("incorrect disk name")
	}

	if bam.Alloc(TS{36, 0}) != OutOfRange {
		t.Error("expected out of range error")
	}
	if bam.Alloc(TS{1, 32}) != OutOfRange {
		t.Error("expected out of range error")
	}
}

func TestExtractDiskFS(t *testing.T) {
	b, err := os.ReadFile("testdata/dc10c.d64")
	if err != nil {
		t.Fatal(err)
	}
	var img Img
	switch {
	case len(b) < len(img):
		t.Fatal("d64 file too small:", len(b))
	case len(b) > len(img):
		t.Fatal("d64 file too big:", len(b))
	default:
		copy(img[:], b)
	}

	if err := os.Chdir("testdata"); err != nil {
		t.Fatal(err)
	}
	diskfs := img.FS()
	defer os.Chdir("..")
	err = fs.WalkDir(diskfs, ".", func (path string, d fs.DirEntry, err error) error {
		t.Log("*DBG*", "path:", path, "err:", err)
		if d == nil || path == "." || (err != nil && !errors.Is(err, fs.ErrExist)) {
			t.Logf("Returning. d:%#v err:%v", d, err)
			return err
		}
		if d.IsDir() {
			t.Log(path, "IsDir")
			if err = os.Mkdir(path, 0755); err != nil && !errors.Is(err, fs.ErrExist) {
				t.Log("Mkdir err", err)
				return err
			}
			return nil
		}
		t.Log(path, "IsFile")
		var rdr io.ReadCloser
		var wtr io.WriteCloser
		if rdr, err = diskfs.Open(path); err != nil {
			t.Log(err)
			return err
		}
		defer rdr.Close()
		if wtr, err = os.Create(path); err != nil {
			t.Log(err)
			return err
		}
		defer wtr.Close()
		if _, err = io.Copy(wtr, rdr); err != nil {
			t.Log(err)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
