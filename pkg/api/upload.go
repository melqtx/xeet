package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	uploadEndpoint = "https://upload.twitter.com/1.1/media/upload.json"
	// uploadChunkSize keeps every APPEND under X's 5 MB segment ceiling.
	uploadChunkSize = 4 << 20
	// chunkedUploadThreshold routes larger-than-simple-upload media (15 MB
	// GIFs, for one) through the chunked flow even when they are not video.
	chunkedUploadThreshold = 5 << 20
	// maxProcessingWait bounds the FINALIZE→STATUS poll; X transcodes most
	// tweet-length videos well inside this.
	maxProcessingWait = 5 * time.Minute
)

// needsChunkedUpload picks the INIT/APPEND/FINALIZE flow for video (which the
// simple endpoint rejects), file-backed uploads, and oversized buffers.
func (u Upload) needsChunkedUpload() bool {
	return u.Path != "" || strings.HasPrefix(u.ContentType, "video/") || len(u.Data) > chunkedUploadThreshold
}

// mediaCategory tells X's pipeline how to transcode and validate the upload;
// omitting it makes video FINALIZE fail.
func mediaCategory(contentType string) string {
	switch {
	case strings.HasPrefix(contentType, "video/"):
		return "tweet_video"
	case contentType == "image/gif":
		return "tweet_gif"
	default:
		return "tweet_image"
	}
}

type processingInfo struct {
	State          string `json:"state"`
	CheckAfterSecs int    `json:"check_after_secs"`
	ProgressPct    int    `json:"progress_percent"`
	Error          struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	} `json:"error"`
}

type uploadStatusResponse struct {
	MediaIDString  string          `json:"media_id_string"`
	ProcessingInfo *processingInfo `json:"processing_info"`
}

// uploadMediaChunked uploads one attachment through the chunked media
// endpoint and waits for server-side processing so the returned media id is
// attachable. progress (optional) reports transmitted bytes.
func (c *WebClient) uploadMediaChunked(ctx context.Context, upload Upload, progress func(sent, total int64)) (string, error) {
	var reader io.Reader
	var total int64
	if upload.Path != "" {
		file, err := os.Open(upload.Path)
		if err != nil {
			return "", fmt.Errorf("open media: %w", err)
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return "", fmt.Errorf("read media: %w", err)
		}
		reader, total = file, info.Size()
	} else {
		reader, total = bytes.NewReader(upload.Data), int64(len(upload.Data))
	}
	if total <= 0 {
		return "", fmt.Errorf("empty media")
	}
	contentType := upload.ContentType
	if contentType == "" {
		return "", fmt.Errorf("media content type is required for chunked upload")
	}

	mediaID, err := c.uploadCommand(ctx, url.Values{
		"command":        {"INIT"},
		"total_bytes":    {strconv.FormatInt(total, 10)},
		"media_type":     {contentType},
		"media_category": {mediaCategory(contentType)},
	})
	if err != nil {
		return "", fmt.Errorf("initialize upload: %w", err)
	}

	buffer := make([]byte, uploadChunkSize)
	var sent int64
	for segment := 0; sent < total; segment++ {
		n, readErr := io.ReadFull(reader, buffer)
		if readErr == io.ErrUnexpectedEOF || readErr == io.EOF {
			readErr = nil
		}
		if readErr != nil {
			return "", fmt.Errorf("read media: %w", readErr)
		}
		if n == 0 {
			return "", fmt.Errorf("media truncated at %d of %d bytes", sent, total)
		}
		if err := c.uploadAppend(ctx, mediaID, segment, upload.Filename, buffer[:n]); err != nil {
			return "", fmt.Errorf("upload segment %d: %w", segment+1, err)
		}
		sent += int64(n)
		if progress != nil {
			progress(sent, total)
		}
	}

	status, err := c.uploadFinalize(ctx, mediaID)
	if err != nil {
		return "", fmt.Errorf("finalize upload: %w", err)
	}
	return c.awaitProcessing(ctx, mediaID, status)
}

// awaitProcessing polls STATUS until X finishes transcoding. Images finish
// synchronously (no processing_info); video usually returns pending.
func (c *WebClient) awaitProcessing(ctx context.Context, mediaID string, status *uploadStatusResponse) (string, error) {
	deadline := time.Now().Add(maxProcessingWait)
	info := status.ProcessingInfo
	for {
		if info == nil || info.State == "succeeded" {
			return mediaID, nil
		}
		if info.State == "failed" {
			message := info.Error.Message
			if message == "" {
				message = info.Error.Name
			}
			if message == "" {
				message = "media processing failed"
			}
			return "", fmt.Errorf("X could not process this media: %s", message)
		}
		wait := time.Duration(info.CheckAfterSecs) * time.Second
		if wait <= 0 {
			wait = time.Second
		}
		if wait > 15*time.Second {
			wait = 15 * time.Second
		}
		if time.Now().Add(wait).After(deadline) {
			return "", fmt.Errorf("media processing did not finish within %s", maxProcessingWait)
		}
		select {
		case <-ctx.Done():
			return "", classifyTransportError(ctx.Err())
		case <-time.After(wait):
		}
		next, err := c.uploadStatus(ctx, mediaID)
		if err != nil {
			return "", fmt.Errorf("check media processing: %w", err)
		}
		info = next.ProcessingInfo
	}
}

// uploadCommand posts a form-encoded INIT or FINALIZE and returns the media
// id from the response.
func (c *WebClient) uploadCommand(ctx context.Context, form url.Values) (string, error) {
	body := form.Encode()
	res, err := c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadEndpoint, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	}, true, 4<<20)
	if err != nil {
		return "", err
	}
	parsed, err := decodeUploadResponse(res)
	if err != nil {
		return "", err
	}
	if parsed.MediaIDString == "" {
		return "", fmt.Errorf("upload returned no media id: %s", truncate(res.body))
	}
	return parsed.MediaIDString, nil
}

// uploadAppend sends one media segment. Retrying a segment is safe: X keys
// segments by index, so a duplicate APPEND overwrites identical bytes.
func (c *WebClient) uploadAppend(ctx context.Context, mediaID string, segment int, filename string, chunk []byte) error {
	name := filepath.Base(filename)
	if name == "." || name == "" {
		name = "media"
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for field, value := range map[string]string{
		"command":       "APPEND",
		"media_id":      mediaID,
		"segment_index": strconv.Itoa(segment),
	} {
		if err := w.WriteField(field, value); err != nil {
			return err
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", multipart.FileContentDisposition("media", name))
	header.Set("Content-Type", "application/octet-stream")
	part, err := w.CreatePart(header)
	if err != nil {
		return err
	}
	if _, err := part.Write(chunk); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	formBody := buf.Bytes()
	res, err := c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadEndpoint, bytes.NewReader(formBody))
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		req.Header.Set("Content-Type", w.FormDataContentType())
		return req, nil
	}, true, 1<<20)
	if err != nil {
		return err
	}
	// APPEND answers 204 with an empty body on success.
	if err := statusToError(res.status, res.header); err != nil {
		return err
	}
	if isTransientStatus(res.status) {
		return &ServiceUnavailableError{Status: res.status}
	}
	if res.status < http.StatusOK || res.status >= http.StatusMultipleChoices {
		return fmt.Errorf("upload HTTP %d: %s", res.status, truncate(res.body))
	}
	return nil
}

func (c *WebClient) uploadFinalize(ctx context.Context, mediaID string) (*uploadStatusResponse, error) {
	body := url.Values{"command": {"FINALIZE"}, "media_id": {mediaID}}.Encode()
	res, err := c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadEndpoint, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return req, nil
	}, true, 4<<20)
	if err != nil {
		return nil, err
	}
	return decodeUploadResponse(res)
}

func (c *WebClient) uploadStatus(ctx context.Context, mediaID string) (*uploadStatusResponse, error) {
	statusURL := uploadEndpoint + "?" + url.Values{"command": {"STATUS"}, "media_id": {mediaID}}.Encode()
	res, err := c.send(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		if err != nil {
			return nil, err
		}
		c.setHeaders(req)
		req.Header.Del("Content-Type")
		return req, nil
	}, true, 4<<20)
	if err != nil {
		return nil, err
	}
	return decodeUploadResponse(res)
}

func decodeUploadResponse(res *httpResult) (*uploadStatusResponse, error) {
	if err := statusToError(res.status, res.header); err != nil {
		return nil, err
	}
	if isTransientStatus(res.status) {
		return nil, &ServiceUnavailableError{Status: res.status}
	}
	if res.status < http.StatusOK || res.status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("upload HTTP %d: %s", res.status, truncate(res.body))
	}
	var parsed uploadStatusResponse
	if len(res.body) > 0 {
		if err := json.Unmarshal(res.body, &parsed); err != nil {
			return nil, fmt.Errorf("decode upload response: %w", err)
		}
	}
	return &parsed, nil
}
