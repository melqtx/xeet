package media

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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
