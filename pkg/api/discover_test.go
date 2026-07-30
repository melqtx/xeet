package api

import "testing"

func TestFindCreateTweetQID(t *testing.T) {
	cases := []struct {
		name string
		js   string
		want string
	}{
		{
			name: "queryId before operationName",
			js:   `foo,{queryId:"AbC123_xY-z",operationName:"CreateTweet",operationType:"mutation"},bar`,
			want: "AbC123_xY-z",
		},
		{
			name: "operationName before queryId",
			js:   `x={operationName:"CreateTweet",operationType:"mutation",queryId:"ZZ99queryIdHere"};`,
			want: "ZZ99queryIdHere",
		},
		{
			name: "other operations present too",
			js:   `{queryId:"otherOP_aaaa",operationName:"HomeTimeline"},{queryId:"realCreate_bbbb",operationName:"CreateTweet"}`,
			want: "realCreate_bbbb",
		},
		{
			name: "not present",
			js:   `{queryId:"nope",operationName:"DeleteTweet"}`,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := findCreateTweetQID(tc.js); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestFindOperationQID(t *testing.T) {
	js := `{queryId:"home-query-123",operationName:"HomeTimeline"}`
	if got := findOperationQID(js, "HomeTimeline"); got != "home-query-123" {
		t.Fatalf("got %q", got)
	}
}

func TestOperationHintSelectsTheBundleForEachOperationFamily(t *testing.T) {
	tests := []struct {
		operation string
		want      string
	}{
		{operation: "TweetDetail", want: "TweetDetail"},
		{operation: "HomeTimeline", want: "HomeTimeline"},
		{operation: "CreateTweet", want: "Compose"},
		{operation: "SearchTimeline", want: "SearchTimeline"},
		{operation: "Bookmarks", want: "Bookmarks"},
		// Both list read operations ship in the shared timeline chunk, observed
		// live as shared~loader.Dock~bundle.BookmarkFolders~bundle.Bookmarks~
		// bundle.Explore~bundle.HomeTimeline~bundle.Notifica.<hash>.js. Its name
		// never contains "List", so hinting "List" reaches only the list
		// management bundles (UserLists, ListHandler) and discovery finds
		// nothing — the failure this table exists to prevent.
		{operation: "ListLatestTweetsTimeline", want: "HomeTimeline"},
		{operation: "ListsManagementPageTimeline", want: "HomeTimeline"},
	}
	for _, test := range tests {
		t.Run(test.operation, func(t *testing.T) {
			if got := operationHint(test.operation); got != test.want {
				t.Fatalf("operationHint(%q) = %q, want %q", test.operation, got, test.want)
			}
		})
	}
}

func TestWebpackChunkURLs(t *testing.T) {
	runtime := `69195:"bundle.HomeTimeline",73796:"shared~bundle.LoggedInMain~bundle.HomeTimeline",69195:"0f610dc",73796:"e992705"`
	urls := webpackChunkURLs(runtime, "HomeTimeline")
	if len(urls) != 2 {
		t.Fatalf("got %v", urls)
	}
}

func TestScriptURLRegex(t *testing.T) {
	html := `<script src="https://abs.twimg.com/responsive-web/client-web/main.abc123.js"></script>` +
		`<link href="https://abs.twimg.com/responsive-web/client-web/shared~bundle.The_Tweet.def456.js">`
	got := scriptURLRe.FindAllString(html, -1)
	if len(got) != 2 {
		t.Fatalf("expected 2 script urls, got %d: %v", len(got), got)
	}
}
