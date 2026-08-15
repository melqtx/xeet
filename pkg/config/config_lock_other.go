//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package config

func lockConfigFile(string) (func(), error) {
	return func() {}, nil
}
