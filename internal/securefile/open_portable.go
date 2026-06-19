//go:build !darwin && !linux

package securefile

import (
	"errors"
	"os"
)

// OpenReadOnly opens a regular file read-only and verifies that the selected
// object did not change across the open operation.
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

	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open file read-only")
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
