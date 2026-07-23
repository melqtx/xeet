package api

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// reconcileCreatedPost performs one read-only timeline lookup after an
// ambiguous CreateTweet response. It never repeats the mutation. Reconciliation
// is deliberately conservative: exactly one recent post must match the text
// and media count, otherwise the result remains ambiguous.
func (c *WebClient) reconcileCreatedPost(
	ctx context.Context,
	text string,
	replyToID string,
	mediaCount int,
	attemptedAt time.Time,
) (string, string) {
	if replyToID != "" {
		return "", "skipped_reply"
	}
	normalized := normalizePostText(text)
	if normalized == "" {
		return "", "skipped_no_text"
	}

	if c.reconcileDelay > 0 {
		timer := time.NewTimer(c.reconcileDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", "cancelled"
		case <-timer.C:
		}
	}

	page, err := c.FetchHomeTimeline(ctx, "", 30)
	if err != nil {
		return "", "read_failed"
	}

	// Allow a little clock skew, but do not accept an old post with identical
	// text. A common phrase could otherwise produce a false success.
	earliest := attemptedAt.Add(-30 * time.Second)
	latest := time.Now().Add(2 * time.Minute)
	var matches []TimelinePost
	for _, post := range page.Posts {
		if post.CreatedAt.IsZero() || post.CreatedAt.Before(earliest) || post.CreatedAt.After(latest) {
			continue
		}
		if normalizePostText(post.Text) != normalized {
			continue
		}
		if len(post.Media) != mediaCount {
			continue
		}
		matches = append(matches, post)
	}
	if len(matches) != 1 {
		if len(matches) > 1 {
			return "", "multiple_matches"
		}
		return "", "not_found"
	}
	if matches[0].ID == "" {
		return "", "missing_id"
	}
	return matches[0].ID, ""
}

func (c *WebClient) finishAmbiguousCreate(
	ctx context.Context,
	text string,
	replyToID string,
	mediaCount int,
	attemptedAt time.Time,
	diagnostic string,
	progress ProgressFunc,
) (string, error) {
	emitProgress(progress, PostEvent{Stage: PostStageReconciling})
	postID, note := c.reconcileCreatedPost(ctx, text, replyToID, mediaCount, attemptedAt)
	if postID != "" {
		c.lastDiagnostic = appendDiagnostic(diagnostic, "reconcile=found")
		emitProgress(progress, PostEvent{Stage: PostStageComplete})
		return postID, nil
	}

	c.lastDiagnostic = appendDiagnostic(diagnostic, "reconcile="+note)
	err := &AmbiguousPostError{Reconciliation: reconciliationNote(note)}
	return "", withDiagnostic(err, c.lastDiagnostic)
}

func appendDiagnostic(base, detail string) string {
	if base == "" {
		return detail
	}
	if detail == "" {
		return base
	}
	return base + " " + detail
}

func normalizePostText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func reconciliationNote(note string) string {
	switch note {
	case "skipped_reply":
		return "reply results cannot be verified safely"
	case "skipped_no_text":
		return "image-only results cannot be verified safely"
	case "cancelled":
		return "verification was cancelled"
	case "read_failed":
		return "the follow-up timeline check failed"
	case "multiple_matches":
		return "more than one recent post matched"
	case "not_found":
		return "the home timeline could not confirm it"
	case "missing_id":
		return "the matching timeline post had no id"
	default:
		if note == "" {
			return ""
		}
		return fmt.Sprintf("verification result: %s", note)
	}
}
