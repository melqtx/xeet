package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/melqtx/xeet/internal/timeline"
)

func TestColumnsFlagRegisteredSeparatelyOnRootAndTimeline(t *testing.T) {
	rootColumnsFlag := rootCmd.Flags().Lookup("columns")
	timelineColumnsFlag := timelineCmd.Flags().Lookup("columns")
	if rootColumnsFlag == nil || timelineColumnsFlag == nil {
		t.Fatalf("--columns must be registered separately on root and timeline: root=%v timeline=%v", rootColumnsFlag, timelineColumnsFlag)
	}
	if rootColumnsFlag == timelineColumnsFlag {
		t.Fatal("root and timeline unexpectedly share one --columns flag")
	}
	if rootColumnsFlag.DefValue != "1" || timelineColumnsFlag.DefValue != "1" {
		t.Fatalf("--columns defaults are root=%q timeline=%q, want 1", rootColumnsFlag.DefValue, timelineColumnsFlag.DefValue)
	}
}

func TestColumnsFlagRejectsValuesOutsideOneToFour(t *testing.T) {
	for _, columns := range []int{-1, 0, 5, 20} {
		err := runTimeline(context.Background(), "off", timeline.FeedForYou, "", "", columns)
		if err == nil || !strings.Contains(err.Error(), "invalid --columns value") ||
			!strings.Contains(err.Error(), "1 to 4") {
			t.Fatalf("--columns %d returned %v", columns, err)
		}
	}
}
