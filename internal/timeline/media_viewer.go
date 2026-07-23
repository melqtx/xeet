package timeline

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"xeet/pkg/api"

	tea "github.com/charmbracelet/bubbletea"
)

const maxViewerImageBytes = 25 << 20

func openPostMedia(post api.TimelinePost) tea.Cmd {
	return func() tea.Msg {
		if len(post.Media) == 0 {
			return actionMsg{err: fmt.Errorf("this post has no viewable images")}
		}
		target := originalMediaURL(post.Media[0].URL)
		parsed, err := url.Parse(target)
		if err != nil {
			return actionMsg{err: fmt.Errorf("image URL: %w", err)}
		}
		if err := validateMediaURL(parsed); err != nil {
			return actionMsg{err: fmt.Errorf("image URL: %w", err)}
		}
		if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return actionMsg{err: fmt.Errorf("no graphical display is available; image URL: %s", target)}
		}

		// feh gives Linux users a proper high-resolution gallery window. It
		// requires an X11 display; Wayland users commonly have one via XWayland.
		if runtime.GOOS == "linux" && os.Getenv("DISPLAY") != "" {
			if feh, err := exec.LookPath("feh"); err == nil {
				return launchFeh(post, feh)
			}
		}

		if err := openExternalURL(target); err != nil {
			return actionMsg{err: fmt.Errorf("open image: %w", err)}
		}
		return actionMsg{message: "opened image in browser"}
	}
}

func launchFeh(post api.TimelinePost, feh string) tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	dir, err := os.MkdirTemp("", "xeet-media-")
	if err != nil {
		return actionMsg{err: fmt.Errorf("prepare image viewer: %w", err)}
	}
	paths, err := downloadTimelineMedia(ctx, dir, post.Media)
	if err != nil {
		os.RemoveAll(dir)
		return actionMsg{err: err}
	}

	args := []string{"--scale-down", "--auto-zoom", "--image-bg", "black"}
	args = append(args, paths...)
	cmd := exec.Command(feh, args...)
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return actionMsg{err: fmt.Errorf("start feh: %w", err)}
	}
	go func() {
		_ = cmd.Wait()
		_ = os.RemoveAll(dir)
	}()
	return actionMsg{message: fmt.Sprintf("opened %d image(s) in feh", len(paths))}
}

func downloadTimelineMedia(ctx context.Context, dir string, media []api.TimelineMedia) ([]string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many image redirects")
			}
			return validateMediaURL(req.URL)
		},
	}
	paths := make([]string, 0, len(media))
	for i, item := range media {
		parsed, err := url.Parse(originalMediaURL(item.URL))
		if err != nil {
			return nil, fmt.Errorf("image %d URL: %w", i+1, err)
		}
		if err := validateMediaURL(parsed); err != nil {
			return nil, fmt.Errorf("image %d URL: %w", i+1, err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("image %d request: %w", i+1, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("download image %d: %w", i+1, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("download image %d: HTTP %d", i+1, resp.StatusCode)
		}
		contentType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
		extension, ok := imageExtension(contentType)
		if !ok {
			resp.Body.Close()
			return nil, fmt.Errorf("download image %d: unsupported content type %q", i+1, contentType)
		}

		path := filepath.Join(dir, fmt.Sprintf("%02d%s", i+1, extension))
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}
		written, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxViewerImageBytes+1))
		closeErr := file.Close()
		resp.Body.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("download image %d: %w", i+1, copyErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if written > maxViewerImageBytes {
			return nil, fmt.Errorf("image %d exceeds the 25 MiB viewer limit", i+1)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func originalMediaURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	query.Set("name", "orig")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func validateMediaURL(parsed *url.URL) error {
	if parsed.Scheme != "https" {
		return fmt.Errorf("only HTTPS image URLs are allowed")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "pbs.twimg.com" && host != "video.twimg.com" && !strings.HasSuffix(host, ".twimg.com") {
		return fmt.Errorf("untrusted image host %q", host)
	}
	return nil
}

func imageExtension(contentType string) (string, bool) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}

func openExternalURL(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}
