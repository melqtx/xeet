package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunInspectHARPrintsOnlySafeShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.har")
	har := `{"log":{"entries":[{"request":{
		"method":"POST",
		"url":"https://x.com/i/api/graphql/private-qid/CreateTweet",
		"headers":[
			{"name":"Cookie","value":"auth_token=private-auth; ct0=private-csrf"},
			{"name":"User-Agent","value":"Mozilla/5.0 (Macintosh) Chrome/126.0"}
		],
		"postData":{"text":"{\"variables\":{\"tweet_text\":\"private-draft\"},\"features\":{},\"queryId\":\"private-qid\"}"}
	}}]}}`
	if err := os.WriteFile(path, []byte(har), 0600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&output)
	if err := runInspectHAR(command, []string{path}); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-auth", "private-csrf", "private-draft", "private-qid"} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("output leaked %q:\n%s", secret, output.String())
		}
	}
	if !strings.Contains(output.String(), "browser: chromium 126 on macos") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}
