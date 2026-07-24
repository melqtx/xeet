package media

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "image-*.png")
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFromClipboard(t *testing.T) {
	a, err := FromClipboard(testPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	if a.Format != "PNG" || a.Width != 3 || a.Height != 2 || a.ID == "" {
		t.Fatalf("unexpected attachment: %+v", a)
	}
}

func TestFromPathDoesNotTrustExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "actually-png.jpg")
	if err := os.WriteFile(path, testPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := FromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if a.Format != "PNG" || a.Name != "actually-png.jpg" {
		t.Fatalf("unexpected attachment: %+v", a)
	}
}

func TestNormalizePath(t *testing.T) {
	got, err := NormalizePath(`./hello\ world.png`)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "hello world.png" {
		t.Fatalf("got %q", got)
	}
}

func TestRejectsGarbage(t *testing.T) {
	if _, err := FromClipboard([]byte("not an image")); err == nil {
		t.Fatal("expected corrupt image error")
	}
}

func TestFromPathDetectsMP4Video(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	data := append([]byte{0, 0, 0, 0x18}, []byte("ftypisom")...)
	data = append(data, make([]byte, 64)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := FromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !a.IsVideo() || a.MIME != "video/mp4" || a.Format != "MP4" {
		t.Fatalf("attachment = %+v, want mp4 video", a)
	}
	if len(a.Data) != 0 || a.Path != path || a.Size != int64(len(data)) {
		t.Fatalf("video must stay path-backed: %+v", a)
	}
}

func TestFromPathDetectsQuickTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mov")
	data := append([]byte{0, 0, 0, 0x14}, []byte("ftypqt  ")...)
	data = append(data, make([]byte, 32)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := FromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if a.MIME != "video/quicktime" || a.Format != "MOV" {
		t.Fatalf("attachment = %+v, want quicktime video", a)
	}
}

func TestFromPathRejectsUnsupportedVideo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.webm")
	if err := os.WriteFile(path, []byte{0x1A, 0x45, 0xDF, 0xA3, 0, 0, 0, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := FromPath(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported video format") {
		t.Fatalf("expected targeted video error, got %v", err)
	}
}
