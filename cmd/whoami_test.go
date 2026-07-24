package cmd

import (
	"bytes"
	"strings"
	"testing"

	"xeet/pkg/api"
)

func TestPrintAccount(t *testing.T) {
	var out bytes.Buffer
	printAccount(&out, &api.Account{
		ID: "42", Name: "Alice Example", Handle: "alice", Verified: true,
	}, "Chrome / Profile 2")
	for _, want := range []string{
		"account: @alice", "name: Alice Example", "id: 42", "verified: yes", "session: Chrome / Profile 2",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestWhoamiCommandRejectsArguments(t *testing.T) {
	if err := whoamiCmd.Args(whoamiCmd, []string{"extra"}); err == nil {
		t.Fatal("whoami accepted an argument")
	}
}
