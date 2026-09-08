package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"unicode/utf8"
)

const (
	StandardPostLimit = 280
	LongPostLimit     = 25000
)

// PostTextLength counts Unicode code points, not terminal display columns.
// This is a draft counter; X applies its own URL and weighted Unicode rules.
func PostTextLength(text string) int { return utf8.RuneCountInString(text) }

func ValidatePostText(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("post text must be valid UTF-8")
	}
	if PostTextLength(text) > LongPostLimit {
		return fmt.Errorf("post exceeds 25,000 characters; shorten it before posting")
	}
	return nil
}

func PostTextCounter(text string) string {
	n := PostTextLength(text)
	if n > StandardPostLimit {
		return fmt.Sprintf("%d chars · 25,000 Premium max", n)
	}
	return fmt.Sprintf("%d chars · 280 standard", n)
}

// Only a definitive, sole length error can authorize a different mutation.
// Never retry success, malformed responses, server failures, or mixed errors.
func postLengthRejected(res *httpResult) bool {
	if res.status != http.StatusOK && res.status != http.StatusBadRequest && res.status != http.StatusForbidden {
		return false
	}
	var payload any
	if json.Unmarshal(res.body, &payload) != nil {
		return false
	}
	if id, _ := createdPostID(payload); id != "" {
		return false
	}
	root, _ := payload.(map[string]any)
	issues := graphQLIssuesFrom(root["errors"])
	all := collectGraphQLIssues(payload)
	return len(issues) == 1 && len(all) == 1 && issues[0].code == 186
}
