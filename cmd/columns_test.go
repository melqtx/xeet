package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
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

func TestColumnsFlagAcceptsCountsAndFeedSpecs(t *testing.T) {
	bookmarks := timeline.ColumnSpec{Kind: timeline.FeedBookmarks}
	tests := []struct {
		name      string
		value     string
		base      timeline.ColumnSpec
		want      []timeline.ColumnSpec
		ownsFeeds bool
	}{
		{
			name: "count repeats selected feed", value: "2", base: bookmarks,
			want: []timeline.ColumnSpec{bookmarks, bookmarks},
		},
		{
			name: "feed list selects each feed", value: "foryou,bookmarks",
			want: []timeline.ColumnSpec{
				{Kind: timeline.FeedForYou},
				{Kind: timeline.FeedBookmarks},
			},
			ownsFeeds: true,
		},
		{
			name: "parameterized feeds preserve values", value: "list:1234567890,search:golang",
			want: []timeline.ColumnSpec{
				{Kind: timeline.FeedList, ListID: "1234567890"},
				{Kind: timeline.FeedSearch, Query: "golang"},
			},
			ownsFeeds: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ownsFeeds, err := parseColumnsFlag(tt.value, tt.base)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) || ownsFeeds != tt.ownsFeeds {
				t.Fatalf("parseColumnsFlag(%q) = %v, %v; want %v, %v", tt.value, got, ownsFeeds, tt.want, tt.ownsFeeds)
			}
		})
	}
}

func TestColumnsFlagRejectsUnknownKindsAndMoreThanFour(t *testing.T) {
	for _, value := range []string{"0", "5", "nonsense", "search:", "list:not-a-number", "foryou,following,bookmarks,list:1,search:go"} {
		_, _, err := parseColumnsFlag(value, timeline.ColumnSpec{Kind: timeline.FeedForYou})
		if err == nil || !strings.Contains(err.Error(), "invalid --columns value") {
			t.Fatalf("--columns %q returned %v", value, err)
		}
	}
}

func TestColumnsFeedSpecsAreExclusiveWithSingleFeedFlagsButCountsAreNot(t *testing.T) {
	base := timeline.ColumnSpec{Kind: timeline.FeedBookmarks}
	specs, err := resolveColumnSpecs("2", true, nil, base, true)
	if err != nil {
		t.Fatalf("--columns 2 --bookmarks returned %v", err)
	}
	if !reflect.DeepEqual(specs, []timeline.ColumnSpec{base, base}) {
		t.Fatalf("--columns 2 --bookmarks = %v", specs)
	}

	if _, err := resolveColumnSpecs("foryou,bookmarks", true, nil, base, true); err == nil ||
		!strings.Contains(err.Error(), "cannot be used") {
		t.Fatalf("feed specs with --bookmarks returned %v", err)
	}
}

func TestColumnsConfigKeyIsUsedWhenFlagAbsent(t *testing.T) {
	base := timeline.ColumnSpec{Kind: timeline.FeedForYou}
	got, err := resolveColumnSpecs(
		"1",
		false,
		[]string{"following", "search:golang"},
		base,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []timeline.ColumnSpec{
		{Kind: timeline.FeedFollowing},
		{Kind: timeline.FeedSearch, Query: "golang"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configured columns = %v, want %v", got, want)
	}
}

func TestExplicitSingleFeedFlagOverridesSavedColumns(t *testing.T) {
	base := timeline.ColumnSpec{Kind: timeline.FeedBookmarks}
	got, err := resolveColumnSpecs(
		"1",
		false,
		[]string{"foryou", "following"},
		base,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []timeline.ColumnSpec{base}) {
		t.Fatalf("explicit bookmarks resolved to %v", got)
	}
}

func TestColumnsSaveCommandValidatesAndWritesCanonicalSpecs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	var out bytes.Buffer
	columnsSaveCmd.SetOut(&out)
	defer columnsSaveCmd.SetOut(nil)

	if err := columnsSaveCmd.RunE(columnsSaveCmd, []string{" foryou, list:123 "}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".xeet.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"columns:", "- foryou", "- list:123"} {
		if !strings.Contains(body, want) {
			t.Fatalf("saved config is missing %q:\n%s", want, body)
		}
	}
	if got := out.String(); got != "saved 2 columns\n" {
		t.Fatalf("save output = %q", got)
	}
}

func TestColumnsCommandIsRegistered(t *testing.T) {
	found := false
	for _, command := range rootCmd.Commands() {
		if command.Name() == "columns" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("columns command is not registered")
	}
	if columnsSaveCmd.Parent() != columnsCmd {
		t.Fatal("columns save is not registered under columns")
	}
}
