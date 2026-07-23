//go:build darwin || linux

package config

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// secureReadFile opens a regular file atomically with O_NOFOLLOW. This closes
// the check/open race that exists when Lstat and ReadFile are separate calls.
func secureReadFile(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open without following symlinks", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open %s: invalid file descriptor", path)
	}
	defer file.Close()

	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, fmt.Errorf("inspect %s: %w", path, err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if err := unix.Fchmod(fd, 0600); err != nil {
		return nil, fmt.Errorf("secure permissions on %s: %w", path, err)
	}
	return io.ReadAll(file)
}

func syncDirectory(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Fsync(fd)
}
