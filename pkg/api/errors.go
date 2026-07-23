package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ErrSessionExpired means X rejected the saved cookies. Callers should tell
// the user to re-run 'xeet auth'; wrapping sites add HTTP detail with %w.
var ErrSessionExpired = errors.New("session expired or invalid — run 'xeet auth' to reconnect")

// RateLimitError means X throttled the request (HTTP 429 or GraphQL code 88).
// Reset is when the limit window ends, when X told us; zero otherwise.
type RateLimitError struct {
	Reset time.Time
}

func (e *RateLimitError) Error() string {
	if e.Reset.IsZero() {
		return "x rate limit hit — wait a few minutes and try again"
	}
	wait := time.Until(e.Reset)
	if wait < time.Minute {
		return "x rate limit hit — resets in under a minute"
	}
	return fmt.Sprintf("x rate limit hit — resets in about %d min", int(wait.Round(time.Minute).Minutes()))
}

// statusToError maps auth and rate-limit HTTP statuses to their well-known
// errors. It returns nil for every other status; callers handle those.
func statusToError(status int, header http.Header) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w (HTTP %d)", ErrSessionExpired, status)
	case http.StatusTooManyRequests:
		return &RateLimitError{Reset: rateLimitReset(header)}
	}
	return nil
}

func rateLimitReset(header http.Header) time.Time {
	value := header.Get("x-rate-limit-reset")
	if value == "" {
		return time.Time{}
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}

// mapGraphQLError turns a GraphQL-level error (returned inside an HTTP 200)
// into the most useful Go error. Known X error codes get first-class handling.
func mapGraphQLError(code int, message string) error {
	switch code {
	case 32, 220: // could not authenticate / credentials no longer active
		return fmt.Errorf("%w (x: %s)", ErrSessionExpired, message)
	case 88:
		return &RateLimitError{}
	case 326:
		return fmt.Errorf("account temporarily locked — log into x.com in your browser to unlock it (x: %s)", message)
	}
	if message == "" {
		return fmt.Errorf("x graphql error (code %d)", code)
	}
	return fmt.Errorf("x graphql error: %s", message)
}

// graphQLError inspects a decoded (map-shaped) GraphQL payload and returns an
// error if X reported one, nil otherwise.
func graphQLError(payload any) error {
	root, _ := payload.(map[string]any)
	errs, _ := root["errors"].([]any)
	if len(errs) == 0 {
		return nil
	}
	first, _ := errs[0].(map[string]any)
	message, _ := first["message"].(string)
	return mapGraphQLError(intValue(first["code"]), message)
}
