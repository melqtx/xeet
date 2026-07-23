//go:build !darwin && !linux

package timeline

// Platforms without the select-based tty probe keep the environment's
// verdict; native detection only fires on kitty/ghostty setups anyway.
func probeNativeGraphics() error { return nil }
