package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

type createTweetOutcome struct {
	id         string
	err        error
	diagnostic string
	ambiguous  bool
}

type graphQLIssue struct {
	code    int
	message string
}

// parseCreateTweetResponse classifies the mutation without retaining or
// exposing its body. X sometimes wraps a created post in a
// TweetWithVisibilityResults object and sometimes reports errors below data
// instead of in the top-level GraphQL errors array.
func parseCreateTweetResponse(res *httpResult) createTweetOutcome {
	var payload any
	decodeErr := json.Unmarshal(res.body, &payload)
	if decodeErr != nil {
		diagnostic := createTweetDiagnostic(res, nil, nil, "")
		if err := statusToError(res.status, res.header); err != nil {
			return createTweetOutcome{err: withDiagnostic(err, diagnostic), diagnostic: diagnostic}
		}
		if isSuccessfulStatus(res.status) || isTransientStatus(res.status) {
			return ambiguousCreateOutcome(diagnostic)
		}
		return createTweetOutcome{
			err:        withDiagnostic(fmt.Errorf("decode create response: %w", decodeErr), diagnostic),
			diagnostic: diagnostic,
		}
	}

	issues := collectGraphQLIssues(payload)
	id, idPath := createdPostID(payload)
	diagnostic := createTweetDiagnostic(res, payload, issues, idPath)

	if err := statusToError(res.status, res.header); err != nil {
		return createTweetOutcome{err: withDiagnostic(err, diagnostic), diagnostic: diagnostic}
	}

	// A trustworthy created id is stronger evidence than an incidental nested
	// error attached to quoted or visibility-wrapped data.
	if id != "" {
		return createTweetOutcome{id: id, diagnostic: diagnostic}
	}

	if len(issues) > 0 {
		err := mapGraphQLError(issues[0].code, issues[0].message)
		var rateLimit *RateLimitError
		if errors.As(err, &rateLimit) && rateLimit.Reset.IsZero() {
			rateLimit.Reset = rateLimitReset(res.header)
		}
		return createTweetOutcome{err: withDiagnostic(err, diagnostic), diagnostic: diagnostic}
	}

	if message := createTweetReason(payload); message != "" {
		err := mapGraphQLError(0, message)
		return createTweetOutcome{err: withDiagnostic(err, diagnostic), diagnostic: diagnostic}
	}

	if isSuccessfulStatus(res.status) || isTransientStatus(res.status) {
		return ambiguousCreateOutcome(diagnostic)
	}

	err := fmt.Errorf("create post HTTP %d", res.status)
	return createTweetOutcome{err: withDiagnostic(err, diagnostic), diagnostic: diagnostic}
}

func ambiguousCreateOutcome(diagnostic string) createTweetOutcome {
	err := withDiagnostic(&AmbiguousPostError{}, diagnostic)
	return createTweetOutcome{err: err, diagnostic: diagnostic, ambiguous: true}
}

func isSuccessfulStatus(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

func createTransportDiagnostic(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "transport=cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "transport=timeout"
	default:
		return "transport=failed"
	}
}

func createdPostID(payload any) (string, string) {
	root, _ := payload.(map[string]any)
	data, _ := root["data"].(map[string]any)
	create, _ := data["create_tweet"].(map[string]any)
	results, _ := create["tweet_results"].(map[string]any)
	result := results["result"]
	if result == nil {
		return "", ""
	}
	return trustedPostID(result, "data.create_tweet.tweet_results.result", 0)
}

// trustedPostID follows only result-wrapper keys. A generic recursive search
// could accidentally return an embedded user, quote, or parent post id.
func trustedPostID(node any, path string, depth int) (string, string) {
	if depth > 4 {
		return "", ""
	}
	object, _ := node.(map[string]any)
	if object == nil {
		return "", ""
	}
	if id, _ := object["rest_id"].(string); id != "" {
		return id, path + ".rest_id"
	}
	for _, key := range []string{"tweet", "result"} {
		if child := object[key]; child != nil {
			if id, found := trustedPostID(child, path+"."+key, depth+1); id != "" {
				return id, found
			}
		}
	}
	if nested, _ := object["tweet_results"].(map[string]any); nested != nil {
		if id, found := trustedPostID(nested["result"], path+".tweet_results.result", depth+1); id != "" {
			return id, found
		}
	}
	return "", ""
}

func collectGraphQLIssues(payload any) []graphQLIssue {
	var issues []graphQLIssue
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			keys := make([]string, 0, len(value))
			for key := range value {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if strings.EqualFold(key, "errors") {
					issues = append(issues, graphQLIssuesFrom(value[key])...)
				}
			}
			for _, key := range keys {
				if !strings.EqualFold(key, "errors") {
					walk(value[key])
				}
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(payload)
	return issues
}

func graphQLIssuesFrom(node any) []graphQLIssue {
	list, _ := node.([]any)
	issues := make([]graphQLIssue, 0, len(list))
	for _, item := range list {
		object, _ := item.(map[string]any)
		if object == nil {
			continue
		}
		issue := graphQLIssue{
			code:    errorCode(object["code"]),
			message: firstString(object, "message", "detail", "reason"),
		}
		if issue.code == 0 && issue.message == "" {
			continue
		}
		issues = append(issues, issue)
	}
	return issues
}

func errorCode(value any) int {
	switch code := value.(type) {
	case float64:
		return int(code)
	case json.Number:
		n, _ := code.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(code)
		return n
	}
	return 0
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, _ := object[key].(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// createTweetReason looks only inside the mutation result and only at
// server-authored reason fields, so it cannot confuse post text for an error.
func createTweetReason(payload any) string {
	root, _ := payload.(map[string]any)
	data, _ := root["data"].(map[string]any)
	create, _ := data["create_tweet"].(map[string]any)
	var find func(any, int) string
	find = func(node any, depth int) string {
		if depth > 5 {
			return ""
		}
		switch value := node.(type) {
		case map[string]any:
			if message := firstString(value, "reason", "message", "detail"); message != "" {
				return message
			}
			keys := make([]string, 0, len(value))
			for key := range value {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if message := find(value[key], depth+1); message != "" {
					return message
				}
			}
		case []any:
			for _, child := range value {
				if message := find(child, depth+1); message != "" {
					return message
				}
			}
		}
		return ""
	}
	return find(create, 0)
}

func createTweetDiagnostic(res *httpResult, payload any, issues []graphQLIssue, idPath string) string {
	sum := sha256.Sum256(res.body)
	parts := []string{
		"status=" + strconv.Itoa(res.status),
		"body=" + hex.EncodeToString(sum[:6]),
	}
	if idPath != "" {
		parts = append(parts, "id_path="+idPath)
	}
	if shape := createTweetShape(payload); shape != "" {
		parts = append(parts, "shape="+shape)
	}

	typeNames := collectTypeNames(payload)
	if len(typeNames) > 0 {
		parts = append(parts, "types="+strings.Join(typeNames, ","))
	}
	if len(issues) > 0 {
		codes := make([]string, 0, len(issues))
		for _, issue := range issues {
			codes = append(codes, strconv.Itoa(issue.code))
		}
		parts = append(parts, "errors="+strings.Join(codes, ","))
	}
	if responseIsRateLimited(res.status, issues) {
		if reset := safeHeaderToken(res.header.Get("x-rate-limit-reset")); reset != "" {
			parts = append(parts, "reset="+reset)
		}
	}
	for _, name := range []string{"x-transaction-id", "x-request-id"} {
		if value := safeHeaderToken(res.header.Get(name)); value != "" {
			parts = append(parts, name+"="+value)
			break
		}
	}
	return strings.Join(parts, " ")
}

func responseIsRateLimited(status int, issues []graphQLIssue) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	for _, issue := range issues {
		if issue.code == 88 {
			return true
		}
	}
	return false
}

func createTweetShape(payload any) string {
	root, _ := payload.(map[string]any)
	if root == nil {
		return "non_object"
	}
	data, _ := root["data"].(map[string]any)
	if data == nil {
		return "no_data"
	}
	create, _ := data["create_tweet"].(map[string]any)
	if create == nil {
		return "no_create_tweet"
	}
	results, _ := create["tweet_results"].(map[string]any)
	if results == nil {
		return "create_keys:" + safeObjectKeys(create)
	}
	result, exists := results["result"]
	if !exists || result == nil {
		return "null_result"
	}
	object, _ := result.(map[string]any)
	if object == nil {
		return "non_object_result"
	}
	return "result_keys:" + safeObjectKeys(object)
}

func safeObjectKeys(object map[string]any) string {
	keys := make([]string, 0, len(object))
	for key := range object {
		if safe := safeHeaderToken(key); safe != "" {
			keys = append(keys, safe)
		}
	}
	sort.Strings(keys)
	if len(keys) > 8 {
		keys = keys[:8]
	}
	if len(keys) == 0 {
		return "empty"
	}
	return strings.Join(keys, ",")
}

func collectTypeNames(payload any) []string {
	seen := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if name, _ := value["__typename"].(string); safeHeaderToken(name) != "" {
				seen[safeHeaderToken(name)] = true
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(payload)
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func safeHeaderToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 96 {
		return ""
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("-_.:", char) {
			continue
		}
		return ""
	}
	return value
}
