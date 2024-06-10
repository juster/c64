package disk

import (
	"io"
	"io/fs"
	"fmt"
	"path"
	"sort"
	"time"
)

type rootFileInfo string

func (di *rootFileInfo) Name() string { return *(*string)(di) }
func (di *rootFileInfo) Size() int64 { return 0 }
func (di *rootFileInfo) Mode() fs.FileMode { return fs.ModeDir | 0777 }
func (di *rootFileInfo) ModTime() time.Time { return time.Time{} }
func (di *rootFileInfo) IsDir() bool { return true }
func (di *rootFileInfo) Sys() interface{} { return nil }

type FileDirEntry interface {
	fs.File
	fs.DirEntry
}

type diskFS struct {
	disk *Img;
	name string;
	files map[string]FileDirEntry;
}

type rootFile string

func (nf *rootFile) Stat() (fs.FileInfo, error) { return (*rootFileInfo)(nf), nil }
func (nf *rootFile) Read(_ []byte) (int, error) { return 0, io.EOF }
func (nf *rootFile) Close() error { return nil }

func (dfs *diskFS) Open(name string) (fs.File, error) {
	if name == "." {
		return (*rootFile)(&name), nil
	}
	dir, file := path.Split(name)
	switch {
	case dir == dfs.name + "/":
		if ent := dfs.files[file]; ent != nil {
			return ent, nil
		}
	case dir == "" && file == dfs.name:
		return (*rootFile)(&file), nil
	}
	return nil, &fs.PathError{"open", name, fs.ErrNotExist}
}

func (dfs *diskFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "." {
		entry := fs.FileInfoToDirEntry((*rootFileInfo)(&dfs.name))
		return []fs.DirEntry{entry}, nil
	}
	if name != dfs.name {
		return nil, fs.ErrNotExist
	}

	var entries []fs.DirEntry
	for _, file := range dfs.files {
		finfo, err := file.Stat()
		if err != nil {
			return nil, err
		}
		entries = append(entries, fs.FileInfoToDirEntry(finfo))
	}
	sort.Slice(entries, func (i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	return entries, nil
}

type dirEntryFile struct {
	disk *Img;
	entry *DirEntry;
	iter FileBlock;
	offset int;
}

// fs.File methods

func (def *dirEntryFile) Stat() (fs.FileInfo, error) {
	return def, nil
}

// Read exports a disk file entry to external file data.

func (f *dirEntryFile) Read(dest []byte) (int, error) {
	var read int
	for read < len(dest) {
		blk := f.iter.Bytes()
		if len(blk) == 0 {
			return read, io.EOF
		}
		n := copy(dest[read:], blk[f.offset:])
		read += n
		if n < len(blk) {
			f.offset = n
			break
		}
		f.offset = 0
		next := f.iter.NextBlock(f.disk)
		if next == nil {
			return read, io.EOF
		}
		f.iter = next
	}
	return read, nil
}

func (def *dirEntryFile) Close() error {
	return nil
}

// fs.FileInfo methods

func (def *dirEntryFile) Name() string {
	var name, ext string
	name = UnpadBytes(def.entry.Filename[:])
	switch def.entry.FileType {
		case DEL: ext = "DEL"
		case SEQ: ext = "SEQ"
		case PRG: ext = "PRG"
		case USR: ext = "USR"
		case REL: ext = "REL"
		default: ext = "???"
	}
	return fmt.Sprintf("%s.%s", name, ext)
}

func (def *dirEntryFile) Size() int64 {
	var n int64
	for iter := def.entry.FileBlock(def.disk); iter != nil; iter = iter.NextBlock(def.disk) {
		n += int64(iter.Len())
	}
	return n
}

func (def *dirEntryFile) Type() fs.FileMode {
	return 0
}

func (def *dirEntryFile) Info() (fs.FileInfo, error) {
	return def, nil
}

func (def *dirEntryFile) Mode() fs.FileMode {
	if def.entry.FileType == PRG {
		return 0755
	} else {
		return 0644
	}
}

func (def *dirEntryFile) ModTime() time.Time {
	return time.Time{}
}

func (def *dirEntryFile) IsDir() bool {
	return false
}

func (def *dirEntryFile) Sys() interface{} {
	return def.entry
}

func (d *Img) FS() fs.FS {
	bam := d.BAM()
	name := UnpadBytes(bam.DiskName[:])
	return &diskFS{d, name, loadDirEntries(d)}
}

func loadDirEntries(d *Img) map[string]FileDirEntry {
	files := make(map[string]FileDirEntry)
	dir := d.Dir()
	for {
		for i := range dir.Files {
			ent := &dir.Files[i]
			entfile := newDirEntryFile(d, ent)
			if entfile == nil {
				continue
			}
			files[entfile.Name()] = entfile
		}
		ts, ok := dir.Next()
		if !ok {
			break
		}
		dir = (*DirBlock)(d.Block(ts))
	}
	return files
}

func newDirEntryFile(d *Img, entry *DirEntry) FileDirEntry {
	if entry.IsScratched() {
		return nil
	}
	blk := entry.FileBlock(d)
	return &dirEntryFile{
		disk: d,
		entry: entry,
		iter: blk,
	}
}
