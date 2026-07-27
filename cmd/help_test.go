package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRootHelpListsEveryVisibleCommand(t *testing.T) {
	tidyGeneratedCommands()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	defer rootCmd.SetOut(nil)

	renderHelp(rootCmd, nil)
	out := buf.String()

	for _, sub := range rootCmd.Commands() {
		if !sub.IsAvailableCommand() {
			continue
		}
		if !strings.Contains(out, sub.Name()) {
			t.Errorf("help does not mention the %q command", sub.Name())
		}
	}
	for _, want := range []string{"start here", "every day", "extras", "usage", "flags", "examples"} {
		if !strings.Contains(out, want) {
			t.Errorf("help is missing the %q section", want)
		}
	}
}

func TestFeedCommandsAppearTogetherInEveryDayHelp(t *testing.T) {
	for _, group := range groupCommands(rootCmd) {
		if group.title != "every day" {
			continue
		}
		seen := map[string]bool{}
		for _, command := range group.commands {
			seen[command.Name()] = true
		}
		for _, want := range []string{"timeline", "lists", "search", "columns"} {
			if !seen[want] {
				t.Fatalf("%q is missing from the every day help group: %v", want, seen)
			}
		}
		return
	}
	t.Fatal("root help has no every day command group")
}

func TestRootHelpHidesHiddenCommands(t *testing.T) {
	tidyGeneratedCommands()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	defer rootCmd.SetOut(nil)

	renderHelp(rootCmd, nil)
	if strings.Contains(buf.String(), "setqid") {
		t.Error("help lists setqid, which is a hidden escape hatch")
	}
}

func TestGroupCommandsSweepsUpUnlistedOnes(t *testing.T) {
	root := &cobra.Command{Use: "xeet"}
	root.AddCommand(
		&cobra.Command{Use: "auth", Run: func(*cobra.Command, []string) {}},
		&cobra.Command{Use: "brand-new", Run: func(*cobra.Command, []string) {}},
		&cobra.Command{Use: "secret", Hidden: true, Run: func(*cobra.Command, []string) {}},
	)

	groups := groupCommands(root)
	var seen []string
	for _, group := range groups {
		for _, sub := range group.commands {
			seen = append(seen, sub.Name())
		}
	}
	if len(seen) != 2 || seen[0] != "auth" {
		t.Fatalf("unexpected grouping: %v", seen)
	}
	last := groups[len(groups)-1]
	if last.commands[len(last.commands)-1].Name() != "brand-new" {
		t.Errorf("an unlisted command should fall into the last group, got %v", seen)
	}
}

func TestCommandHelpShowsFlagsAndExamples(t *testing.T) {
	var buf bytes.Buffer
	authCmd.SetOut(&buf)
	defer authCmd.SetOut(nil)

	renderHelp(authCmd, nil)
	out := buf.String()

	for _, want := range []string{"xeet auth", "--browser", "examples", "show this help"} {
		if !strings.Contains(out, want) {
			t.Errorf("auth help is missing %q:\n%s", want, out)
		}
	}
}

func TestHelpLeavesNoTrailingWhitespace(t *testing.T) {
	tidyGeneratedCommands()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	defer rootCmd.SetOut(nil)

	renderHelp(rootCmd, nil)
	for i, line := range strings.Split(buf.String(), "\n") {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("line %d of the help ends in whitespace: %q", i+1, line)
		}
	}
}
