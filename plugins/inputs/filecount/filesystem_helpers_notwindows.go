//go:build !windows

// TODO: These types are not used in Windows tests because they are disabled for Windows.
// They can be moved to filesystem_helpers.go when following bug is fixed:
// https://github.com/influxdata/telegraf/issues/6248

package filecount

import (
	"errors"
	"os"
	"time"
)

/*
	The following are for mocking the filesystem - this allows us to mock Stat() files. This means that we can set file attributes, and know that they
	will be the same regardless of the platform sitting underneath our tests (directory sizes vary see https://github.com/influxdata/telegraf/issues/6011)

	NOTE: still need the on-disk file structure to mirror this because the 3rd party library ("github.com/karrick/godirwalk") uses its own
	walk functions, that we cannot mock from here.
*/

type fakeFileSystem struct {
	files map[string]fakeFileInfo
}

type fakeFileInfo struct {
	name     string
	size     int64
	filemode uint32
	modtime  time.Time
	isdir    bool
	sys      interface{}
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return os.FileMode(f.filemode) }
func (f fakeFileInfo) ModTime() time.Time { return f.modtime }
func (f fakeFileInfo) IsDir() bool        { return f.isdir }
func (f fakeFileInfo) Sys() interface{}   { return f.sys }

func (fakeFileSystem) open(name string) (file, error) {
	return nil, &os.PathError{Op: "Open", Path: name, Err: errors.New("not implemented by fake filesystem")}
}

func (f fakeFileSystem) stat(name string) (os.FileInfo, error) {
	if fakeInfo, found := f.files[name]; found {
		return fakeInfo, nil
	}
	return nil, &os.PathError{Op: "Stat", Path: name, Err: errors.New("no such file or directory")}
}

func (f fakeFileSystem) lstat(name string) (os.FileInfo, error) {
	// not able to test with symlinks currently
	return f.stat(name)
}
