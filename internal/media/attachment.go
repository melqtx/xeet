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
	MaxVideoBytes  = 512 << 20
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

// IsVideo reports whether the attachment streams from disk as video rather
// than travelling in Data as an image.
func (a Attachment) IsVideo() bool {
	return strings.HasPrefix(a.MIME, "video/")
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
		return Attachment{}, fmt.Errorf("open media: %w", err)
	}
	defer f.Close()

	head := make([]byte, 16)
	headLen, err := io.ReadFull(f, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return Attachment{}, fmt.Errorf("read media: %w", err)
	}
	head = head[:headLen]
	if mime := videoMIME(head); mime != "" {
		return videoFromFile(f, path, mime)
	}
	rest, err := io.ReadAll(io.LimitReader(f, MaxGIFBytes+1))
	if err != nil {
		return Attachment{}, fmt.Errorf("read image: %w", err)
	}
	data := append(head, rest...)
	if len(data) > MaxGIFBytes {
		return Attachment{}, fmt.Errorf("image is larger than %s", HumanBytes(MaxGIFBytes))
	}
	return fromBytes(filepath.Base(path), path, SourceFile, data)
}

// videoMIME sniffs the container from the file's first bytes. MP4 and
// QuickTime are the formats X accepts; other known video magic returns
// "" here and surfaces a targeted error from fromBytes via looksLikeVideo.
func videoMIME(head []byte) string {
	if len(head) < 12 || string(head[4:8]) != "ftyp" {
		return ""
	}
	if string(head[8:10]) == "qt" {
		return "video/quicktime"
	}
	return "video/mp4"
}

// looksLikeVideo recognizes common non-MP4 video containers so the error can
// say "convert it" instead of "corrupt image".
func looksLikeVideo(head []byte) bool {
	if len(head) >= 4 && head[0] == 0x1A && head[1] == 0x45 && head[2] == 0xDF && head[3] == 0xA3 {
		return true // Matroska/WebM
	}
	if len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "AVI " {
		return true
	}
	return false
}

// videoFromFile builds a path-backed attachment: video never loads into
// memory here; the uploader streams it from disk in chunks.
func videoFromFile(f *os.File, path, mime string) (Attachment, error) {
	info, err := f.Stat()
	if err != nil {
		return Attachment{}, fmt.Errorf("read video: %w", err)
	}
	if info.Size() > MaxVideoBytes {
		return Attachment{}, fmt.Errorf("videos must be %s or smaller", HumanBytes(MaxVideoBytes))
	}
	if info.Size() == 0 {
		return Attachment{}, fmt.Errorf("video is empty")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano())))
	format := "MP4"
	if mime == "video/quicktime" {
		format = "MOV"
	}
	return Attachment{
		ID:     hex.EncodeToString(sum[:6]),
		Name:   filepath.Base(path),
		Source: SourceFile,
		Path:   path,
		MIME:   mime,
		Format: format,
		Size:   info.Size(),
	}, nil
}

func fromBytes(name, path string, source Source, data []byte) (Attachment, error) {
	if len(data) == 0 {
		return Attachment{}, fmt.Errorf("image is empty")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if looksLikeVideo(data) {
			return Attachment{}, fmt.Errorf("unsupported video format; X accepts MP4 (H.264/AAC) or MOV")
		}
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
