//go:build darwin || linux

package timeline

import (
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

// probeNativeGraphics verifies the terminal really loads images over the
// kitty graphics protocol before the timeline reserves rows for native
// previews. Multiplexers such as cmux inherit ghostty's TERM without always
// passing graphics through, and preview transmissions run with q=2 (errors
// suppressed), so trusting the environment alone can leave blank holes where
// images belong. The query transmits a real PNG temp file because that is
// exactly how previews travel (t=f): a confirmation also proves the emulator
// process can read files this process writes.
func probeNativeGraphics() error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open terminal: %w", err)
	}
	defer tty.Close()

	path, err := writePreviewPNG(image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	if err != nil {
		return err
	}
	defer os.Remove(path)

	state, err := term.MakeRaw(tty.Fd())
	if err != nil {
		return fmt.Errorf("enter raw mode: %w", err)
	}
	defer term.Restore(tty.Fd(), state) //nolint:errcheck

	payload := base64.StdEncoding.EncodeToString([]byte(path))
	query := fmt.Sprintf("\x1b_Ga=q,i=%d,f=100,t=f;%s\x1b\\\x1b[c", probeImageID, payload)
	if _, err := tty.WriteString(query); err != nil {
		return fmt.Errorf("write probe: %w", err)
	}

	deadline := time.Now().Add(probeTimeout)
	fd := int(tty.Fd())
	var buffer []byte
	chunk := make([]byte, 128)
	for {
		readable, err := waitReadable(fd, deadline)
		if err != nil {
			return err
		}
		if !readable {
			return fmt.Errorf("terminal did not answer the graphics probe")
		}
		n, readErr := tty.Read(chunk)
		buffer = append(buffer, chunk[:n]...)
		confirmed, done := parseProbeReply(buffer)
		if done {
			if confirmed {
				return nil
			}
			return fmt.Errorf("terminal rejected the graphics probe")
		}
		if readErr != nil {
			return fmt.Errorf("read probe reply: %w", readErr)
		}
	}
}

func waitReadable(fd int, deadline time.Time) (bool, error) {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false, nil
		}
		timeout := unix.NsecToTimeval(remaining.Nanoseconds())
		var fds unix.FdSet
		fds.Set(fd)
		count, err := unix.Select(fd+1, &fds, nil, nil, &timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("wait for probe reply: %w", err)
		}
		return count > 0, nil
	}
}
