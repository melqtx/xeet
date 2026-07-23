package timeline

import (
	"net/url"
	"testing"
)

func TestOriginalMediaURL(t *testing.T) {
	got := originalMediaURL("https://pbs.twimg.com/media/abc?format=jpg&name=small")
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("format") != "jpg" || parsed.Query().Get("name") != "orig" {
		t.Fatalf("originalMediaURL() = %q", got)
	}
}

func TestValidateMediaURL(t *testing.T) {
	for _, raw := range []string{
		"https://pbs.twimg.com/media/abc",
		"https://video.twimg.com/thumb/abc",
	} {
		parsed, _ := url.Parse(raw)
		if err := validateMediaURL(parsed); err != nil {
			t.Errorf("trusted URL %q rejected: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"http://pbs.twimg.com/media/abc",
		"https://example.com/image.jpg",
		"file:///etc/passwd",
	} {
		parsed, _ := url.Parse(raw)
		if err := validateMediaURL(parsed); err == nil {
			t.Errorf("unsafe URL %q accepted", raw)
		}
	}
}

func TestImageExtension(t *testing.T) {
	for mime, want := range map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/gif":  ".gif",
		"image/webp": ".webp",
	} {
		got, ok := imageExtension(mime)
		if !ok || got != want {
			t.Errorf("imageExtension(%q) = %q, %v", mime, got, ok)
		}
	}
	if _, ok := imageExtension("text/html"); ok {
		t.Fatal("accepted text/html as an image")
	}
}
