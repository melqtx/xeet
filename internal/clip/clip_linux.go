//go:build linux

package clip

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"golang.design/x/clipboard"
)

type backend int

const (
	backendNone backend = iota
	backendWayland
	backendX11
)

var state struct {
	once    sync.Once
	backend backend
	err     error
}

// Init picks Wayland tools when running in Wayland, then falls back to the X11
// clipboard. tmux and Zellij inherit either display socket. SSH and headless
// sessions receive an actionable error instead of losing file-attach support.
func Init() error {
	state.once.Do(func() {
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			if _, err := exec.LookPath("wl-paste"); err == nil {
				state.backend = backendWayland
				return
			}
		}
		if err := clipboard.Init(); err == nil {
			state.backend = backendX11
			return
		} else {
			state.err = fmt.Errorf("no usable clipboard backend: install wl-clipboard on Wayland or provide a working DISPLAY for X11: %w", err)
		}
	})
	return state.err
}

func ReadImage() []byte {
	if Init() != nil {
		return nil
	}
	if state.backend == backendX11 {
		return clipboard.Read(clipboard.FmtImage)
	}
	for _, mime := range []string{"image/png", "image/jpeg", "image/webp"} {
		if data, err := exec.Command("wl-paste", "--no-newline", "--type", mime).Output(); err == nil && len(data) > 0 {
			return data
		}
	}
	return nil
}

func ReadText() string {
	if Init() != nil {
		return ""
	}
	if state.backend == backendX11 {
		return string(clipboard.Read(clipboard.FmtText))
	}
	for _, mime := range []string{"text/plain;charset=utf-8", "text/plain", "UTF8_STRING"} {
		if data, err := exec.Command("wl-paste", "--no-newline", "--type", mime).Output(); err == nil && len(data) > 0 {
			return string(data)
		}
	}
	return ""
}

func WriteText(text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("cannot copy empty text")
	}
	if err := Init(); err != nil {
		return err
	}
	if state.backend == backendX11 {
		clipboard.Write(clipboard.FmtText, []byte(text))
		return nil
	}
	path, err := exec.LookPath("wl-copy")
	if err != nil {
		return fmt.Errorf("wl-copy is required to copy text on Wayland: %w", err)
	}
	cmd := exec.Command(path, "--type", "text/plain;charset=utf-8")
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("copying text with wl-copy: %w", err)
	}
	return nil
}
