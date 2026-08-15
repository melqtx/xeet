package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/melqtx/xeet/internal/ui"
	"github.com/melqtx/xeet/pkg/api"
)

func newTestAuthPicker(t *testing.T, browser string) authPicker {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return newAuthPicker(ctx, cancel, ui.Default(), browser)
}

func updateAuth(t *testing.T, p authPicker, msg any) authPicker {
	t.Helper()
	next, _ := p.Update(msg)
	picker, ok := next.(authPicker)
	if !ok {
		t.Fatalf("Update returned %T, not an authPicker", next)
	}
	return picker
}

func TestMatchBrowserIgnoresCase(t *testing.T) {
	name, ok := matchBrowser("firefox")
	if !ok || name != "Firefox" {
		t.Fatalf("matchBrowser(\"firefox\") = %q, %v; want Firefox, true", name, ok)
	}
	if _, ok := matchBrowser("safari"); ok {
		t.Error("matchBrowser accepted an unsupported browser")
	}
}

func TestAuthPickerStartsOnABrowserThatHasASession(t *testing.T) {
	p := newTestAuthPicker(t, "")
	if !p.scanning {
		t.Fatal("the picker should scan for sessions on open")
	}
	// Whatever the supported list is, the second entry stands in for "not the
	// one the cursor starts on".
	detected := p.browsers[1]
	p = updateAuth(t, p, browsersScannedMsg{detected})

	if p.scanning {
		t.Error("scanning should be over once the results arrive")
	}
	if p.browsers[p.cursor] != detected {
		t.Fatalf("cursor sits on %q, want the detected %q", p.browsers[p.cursor], detected)
	}
	if !strings.Contains(p.View(), "session found") {
		t.Error("a detected browser should be marked in the list")
	}
}

func TestAuthPickerLeavesTheCursorAloneWhenNothingIsDetected(t *testing.T) {
	p := newTestAuthPicker(t, "")
	p = updateAuth(t, p, browsersScannedMsg{})

	if p.cursor != 0 {
		t.Fatalf("cursor moved to %d with nothing detected", p.cursor)
	}
	if !strings.Contains(p.View(), "not detected") {
		t.Error("undetected browsers should still be pickable, and labelled")
	}
}

func TestAuthPickerEnterStartsTheImport(t *testing.T) {
	p := newTestAuthPicker(t, "")
	p = updateAuth(t, p, browsersScannedMsg{})
	p = updateAuth(t, p, key("enter"))

	if p.phase != authPhaseImport {
		t.Fatalf("phase is %v, want import", p.phase)
	}
	if p.chosen != p.browsers[0] {
		t.Fatalf("chose %q, want %q", p.chosen, p.browsers[0])
	}
	if !strings.Contains(p.View(), "unlock secure storage") {
		t.Error("the import step should warn about the secure-storage prompt")
	}
}

// --browser is for people who already know the answer, so it skips the list.
func TestAuthPickerWithABrowserSkipsThePicker(t *testing.T) {
	p := newTestAuthPicker(t, "Firefox")
	if p.phase != authPhaseImport || p.chosen != "Firefox" {
		t.Fatalf("phase %v, chosen %q; want import of Firefox", p.phase, p.chosen)
	}
	if p.scanning {
		t.Error("there is nothing to scan for when the browser is already named")
	}
}

func TestAuthPickerOffersAnotherBrowserAfterAFailure(t *testing.T) {
	p := newTestAuthPicker(t, "")
	p = updateAuth(t, p, browsersScannedMsg{})
	p = updateAuth(t, p, key("enter"))
	p = updateAuth(t, p, sessionImportedMsg{err: errors.New("no logged-in x.com session found in Chrome")})

	if p.phase != authPhaseFail {
		t.Fatalf("phase is %v, want fail", p.phase)
	}
	view := p.View()
	if !strings.Contains(view, "no logged-in x.com session") {
		t.Errorf("the failure should say what went wrong:\n%s", view)
	}
	if !strings.Contains(view, "try another browser") {
		t.Errorf("the failure should offer a retry:\n%s", view)
	}

	p = updateAuth(t, p, key("enter"))
	if p.phase != authPhasePick || p.err != nil {
		t.Fatalf("enter after a failure should reopen the picker, got phase %v err %v", p.phase, p.err)
	}
}

func TestAuthPickerVerifiesBeforeItCelebrates(t *testing.T) {
	p := newTestAuthPicker(t, "Chrome")
	p = updateAuth(t, p, sessionImportedMsg{result: &api.LoginResult{}, browser: "Chrome"})
	if p.phase != authPhaseVerify {
		t.Fatalf("phase is %v, want verify after a successful import", p.phase)
	}

	p = updateAuth(t, p, sessionSavedMsg{conn: connection{browser: "Chrome", profile: "Default", handle: "melqtx"}})
	view := p.View()
	if !strings.Contains(view, "@melqtx") {
		t.Errorf("the success panel should name the account:\n%s", view)
	}
	if !strings.Contains(view, `Chrome profile "Default"`) {
		t.Errorf("the success panel should say where the session came from:\n%s", view)
	}
}

func TestAuthPickerCancelLeavesNothingBehind(t *testing.T) {
	p := newTestAuthPicker(t, "")
	p = updateAuth(t, p, key("esc"))
	if !p.quit {
		t.Fatal("escape should quit the picker")
	}
	if p.View() != "" {
		t.Error("a cancelled picker should leave nothing on screen")
	}
}

func TestConnectionSourceNamesTheProfile(t *testing.T) {
	with := connection{browser: "Chrome", profile: "Work"}
	if with.source() != `Chrome profile "Work"` {
		t.Errorf("source() = %q", with.source())
	}
	without := connection{browser: "Firefox"}
	if without.source() != "Firefox" {
		t.Errorf("source() = %q", without.source())
	}
}
