package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrSessionExpired means X rejected the saved cookies. Callers should tell
// the user to re-run 'xeet auth'; wrapping sites add HTTP detail with %w.
var ErrSessionExpired = errors.New("session expired or invalid; run 'xeet auth' to reconnect")

// AutomationBlockedError means X accepted the HTTP request but rejected this
// post as suspected automation or spam. The decision can be content-sensitive,
// so callers must not claim the whole session is blocked or retry the mutation
// automatically.
type AutomationBlockedError struct {
	Message string
}

func (e *AutomationBlockedError) Error() string {
	return "x rejected this post as suspected automation; try the exact text in X"
}

// PostingRestrictedError means X rejected this particular post under an
// account or content posting restriction.
type PostingRestrictedError struct{}

func (e *PostingRestrictedError) Error() string {
	return "x rejected this post under a posting restriction; edit it or try again later"
}

// RecentlyPostedError means X classified the text as recently posted. Xeet
// attributes that decision to X instead of claiming the posts are identical.
type RecentlyPostedError struct{}

func (e *RecentlyPostedError) Error() string {
	return "x rejected this text as recently posted; edit it or try again later"
}

// AmbiguousPostError means the CreateTweet mutation returned without a
// trustworthy success id or a structured rejection. The post may exist, so
// callers must preserve the draft and must not automatically retry.
type AmbiguousPostError struct {
	Reconciliation string
}

func (e *AmbiguousPostError) Error() string {
	message := "x did not confirm this post. Check your X profile before trying again"
	if e.Reconciliation != "" {
		message += " (" + e.Reconciliation + ")"
	}
	return message
}

// DiagnosticError is implemented by errors that carry a sanitized diagnostic
// string. Diagnostics contain response shape and request metadata only, never
// cookies or post contents.
type DiagnosticError interface {
	error
	Diagnostic() string
}

type diagnosticError struct {
	err        error
	diagnostic string
}

func (e *diagnosticError) Error() string      { return e.err.Error() }
func (e *diagnosticError) Unwrap() error      { return e.err }
func (e *diagnosticError) Diagnostic() string { return e.diagnostic }

func withDiagnostic(err error, diagnostic string) error {
	if err == nil || diagnostic == "" {
		return err
	}
	return &diagnosticError{err: err, diagnostic: diagnostic}
}

// ErrorDiagnostic returns safe response-shape detail attached to err.
func ErrorDiagnostic(err error) string {
	var diagnostic DiagnosticError
	if errors.As(err, &diagnostic) {
		return diagnostic.Diagnostic()
	}
	return ""
}

// RateLimitError means X throttled the request (HTTP 429 or GraphQL code 88).
// Reset is when the limit window ends, when X told us; zero otherwise.
type RateLimitError struct {
	Reset time.Time
}

func (e *RateLimitError) Error() string {
	if e.Reset.IsZero() {
		return "x rate limit hit; wait a few minutes and try again"
	}
	wait := time.Until(e.Reset)
	if wait < time.Minute {
		return "x rate limit hit; resets in under a minute"
	}
	return fmt.Sprintf("x rate limit hit; resets in about %d min", int(wait.Round(time.Minute).Minutes()))
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
	lowerMessage := strings.ToLower(message)
	if strings.Contains(lowerMessage, "looks like it might be automated") ||
		strings.Contains(lowerMessage, "protect our users from spam") {
		return &AutomationBlockedError{Message: message}
	}
	switch code {
	case 32, 220: // could not authenticate / credentials no longer active
		return fmt.Errorf("%w (x: %s)", ErrSessionExpired, message)
	case 88:
		return &RateLimitError{}
	case 187:
		return &RecentlyPostedError{}
	case 226:
		return &AutomationBlockedError{Message: message}
	case 344:
		return &PostingRestrictedError{}
	case 326:
		return fmt.Errorf("account temporarily locked. Log into x.com in your browser to unlock it (x: %s)", message)
	}
	if message == "" {
		return fmt.Errorf("x graphql error (code %d)", code)
	}
	return fmt.Errorf("x graphql error: %s", message)
}

// graphQLError inspects only the response-level GraphQL errors array. Nested
// content errors must not make an otherwise valid timeline or lookup fail.
// CreateTweet uses its own parser for mutation-specific nested errors.
func graphQLError(payload any) error {
	root, _ := payload.(map[string]any)
	issues := graphQLIssuesFrom(root["errors"])
	if len(issues) == 0 {
		return nil
	}
	return mapGraphQLError(issues[0].code, issues[0].message)
}
