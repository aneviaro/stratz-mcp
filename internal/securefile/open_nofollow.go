//go:build darwin || linux

// Package securefile opens explicitly selected files without following
// symlinks where the operating system provides O_NOFOLLOW.
package securefile

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// OpenReadOnly opens a regular file read-only and atomically rejects symlinks.
func OpenReadOnly(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("cannot inspect file")
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("symlinks are not allowed")
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}

	descriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, errors.New("cannot open file read-only without following symlinks")
	}
	file := os.NewFile(uintptr(descriptor), path)
	if file == nil {
		_ = unix.Close(descriptor)
		return nil, errors.New("cannot create file handle")
	}

	opened, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, errors.New("cannot inspect opened file")
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		_ = file.Close()
		return nil, errors.New("file changed while it was being opened")
	}
	return file, nil
}
