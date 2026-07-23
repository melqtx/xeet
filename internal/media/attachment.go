package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
)

const (
	MaxAttachments = 4
	MaxImageBytes  = 5 << 20
	MaxGIFBytes    = 15 << 20
)

type Source string

const (
	SourceClipboard Source = "clipboard"
	SourceFile      Source = "file"
)

type Attachment struct {
	ID     string
	Name   string
	Source Source
	Path   string
	MIME   string
	Format string
	Width  int
	Height int
	Size   int64
	Data   []byte
}

func FromClipboard(data []byte) (Attachment, error) {
	return fromBytes("Clipboard image", "", SourceClipboard, data)
}

func FromPath(input string) (Attachment, error) {
	path, err := NormalizePath(input)
	if err != nil {
		return Attachment{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Attachment{}, fmt.Errorf("open image: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, MaxGIFBytes+1))
	if err != nil {
		return Attachment{}, fmt.Errorf("read image: %w", err)
	}
	if len(data) > MaxGIFBytes {
		return Attachment{}, fmt.Errorf("image is larger than %s", HumanBytes(MaxGIFBytes))
	}
	return fromBytes(filepath.Base(path), path, SourceFile, data)
}

func fromBytes(name, path string, source Source, data []byte) (Attachment, error) {
	if len(data) == 0 {
		return Attachment{}, fmt.Errorf("image is empty")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Attachment{}, fmt.Errorf("unsupported or corrupt image: %w", err)
	}
	format = strings.ToLower(format)
	if format != "png" && format != "jpeg" && format != "gif" && format != "webp" {
		return Attachment{}, fmt.Errorf("unsupported image format %q", format)
	}
	limit := MaxImageBytes
	if format == "gif" {
		limit = MaxGIFBytes
	}
	if len(data) > limit {
		return Attachment{}, fmt.Errorf("%s images must be %s or smaller", strings.ToUpper(format), HumanBytes(limit))
	}

	sum := sha256.Sum256(data)
	mime := http.DetectContentType(data)
	if format == "jpeg" {
		mime = "image/jpeg"
	}
	return Attachment{
		ID:     hex.EncodeToString(sum[:6]),
		Name:   name,
		Source: source,
		Path:   path,
		MIME:   mime,
		Format: strings.ToUpper(format),
		Width:  cfg.Width,
		Height: cfg.Height,
		Size:   int64(len(data)),
		Data:   data,
	}, nil
}

func NormalizePath(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", fmt.Errorf("enter an image path")
	}
	if unquoted, err := strconv.Unquote(value); err == nil {
		value = unquoted
	} else if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		value = value[1 : len(value)-1]
	}
	if strings.HasPrefix(value, "file://") {
		u, err := url.Parse(value)
		if err != nil {
			return "", fmt.Errorf("invalid file URL: %w", err)
		}
		value, err = url.PathUnescape(u.Path)
		if err != nil {
			return "", fmt.Errorf("invalid file URL: %w", err)
		}
	}
	// Paths dragged into common terminals escape spaces and punctuation.
	replacer := strings.NewReplacer(`\ `, " ", `\(`, "(", `\)`, ")", `\[`, "[", `\]`, "]", `\&`, "&")
	value = replacer.Replace(value)
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	path, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return "", fmt.Errorf("resolve image path: %w", err)
	}
	return path, nil
}

func HumanBytes(n int) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
