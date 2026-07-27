package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestListsCommandRegisteredWithFlags(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"lists"})
	if err != nil {
		t.Fatalf("find lists command: %v", err)
	}
	if command != listsCmd {
		t.Fatalf("lists resolves to %q instead of listsCmd", command.CommandPath())
	}
	for _, name := range []string{"images", "theme"} {
		if listsCmd.Flags().Lookup(name) == nil {
			t.Errorf("lists command is missing --%s", name)
		}
	}

	rootList := rootCmd.Flags().Lookup("list")
	timelineList := timelineCmd.Flags().Lookup("list")
	if rootList == nil || timelineList == nil {
		t.Fatalf("--list must be registered separately on root and timeline: root=%v timeline=%v", rootList, timelineList)
	}
	if rootList == timelineList {
		t.Fatal("root and timeline unexpectedly share one --list flag")
	}
}

func TestTimelineListFlagIsExclusiveWithOtherFeeds(t *testing.T) {
	tests := []struct {
		name    string
		command *cobra.Command
		other   string
	}{
		{name: "timeline following", command: timelineCmd, other: "following"},
		{name: "timeline bookmarks", command: timelineCmd, other: "bookmarks"},
		{name: "root following", command: rootCmd, other: "following"},
		{name: "root bookmarks", command: rootCmd, other: "bookmarks"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setChangedFlag(t, tt.command, "list", "123")
			setChangedFlag(t, tt.command, tt.other, "true")
			err := tt.command.ValidateFlagGroups()
			if err == nil || !strings.Contains(err.Error(), "none of the others can be") {
				t.Fatalf("--list and --%s were not rejected as mutually exclusive: %v", tt.other, err)
			}
		})
	}
}

func TestListIDMustBeNumeric(t *testing.T) {
	for _, id := range []string{"123", "00123"} {
		if !isNumericListID(id) {
			t.Errorf("numeric list id %q was rejected", id)
		}
	}
	for _, id := range []string{"", "abc", "12.3", "-123", "+123"} {
		if isNumericListID(id) {
			t.Errorf("non-numeric list id %q was accepted", id)
		}
	}
}

func setChangedFlag(t *testing.T, command *cobra.Command, name, value string) {
	t.Helper()
	flag := command.Flags().Lookup(name)
	if flag == nil {
		t.Fatalf("%s has no --%s flag", command.CommandPath(), name)
	}
	oldValue := flag.Value.String()
	oldChanged := flag.Changed
	if err := command.Flags().Set(name, value); err != nil {
		t.Fatalf("set --%s: %v", name, err)
	}
	t.Cleanup(func() {
		if err := command.Flags().Set(name, oldValue); err != nil {
			t.Errorf("restore --%s: %v", name, err)
		}
		flag.Changed = oldChanged
	})
}
