package timeline

import (
	"strings"
	"time"
)

const (
	// probeImageID is only referenced by the startup query; real previews
	// use the 1..255 range, so the probe can never collide with them.
	probeImageID = 4242
	probeTimeout = 2 * time.Second
)

// parseProbeReply scans the terminal's answer to the startup graphics probe.
// A terminal that renders kitty graphics acknowledges the a=q query with
// ESC _ G i=…;OK ESC \ before it answers the DA1 fence (ESC [ ? … c).
// Every terminal answers DA1, so a finished probe without the graphics
// acknowledgement means the query was ignored or rejected.
func parseProbeReply(buffer []byte) (confirmed, done bool) {
	reply := string(buffer)
	if i := strings.Index(reply, "\x1b_G"); i >= 0 && strings.Contains(reply[i:], ";OK") {
		confirmed = true
	}
	if i := strings.Index(reply, "\x1b[?"); i >= 0 && strings.IndexByte(reply[i+3:], 'c') >= 0 {
		done = true
	}
	return confirmed, done
}
