//go:build !darwin && !linux

package config

import (
	"fmt"
	"os"
)

func secureReadFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file; refusing to follow it", path)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func syncDirectory(string) error { return nil }
