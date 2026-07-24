package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/melqtx/xeet/pkg/config"
)

func TestFetchViewerParsesCurrentAccount(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/Viewer") {
			t.Fatalf("path = %q", req.URL.Path)
		}
		if req.URL.Query().Get("variables") == "" || req.URL.Query().Get("features") == "" {
			t.Fatalf("missing Viewer parameters: %s", req.URL.RawQuery)
		}
		return response(http.StatusOK, `{
			"data":{"viewer":{"user_results":{"result":{
				"rest_id":"42","is_blue_verified":true,
				"core":{"name":"Alice Example","screen_name":"alice"}
			}}}}
		}`), nil
	})
	account, err := client.FetchViewer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if account.ID != "42" || account.Name != "Alice Example" || account.Handle != "alice" || !account.Verified {
		t.Fatalf("account = %+v", account)
	}
}

func TestFetchViewerSupportsLegacyShape(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"data":{"viewer":{"result":{"rest_id":"7","legacy":{"name":"Bob","screen_name":"bob","verified":true}}}}}`), nil
	})
	account, err := client.FetchViewer(context.Background())
	if err != nil || account.Handle != "bob" || account.Name != "Bob" || !account.Verified {
		t.Fatalf("account=%+v err=%v", account, err)
	}
}

func TestFetchViewerRefreshesAndExposesRotatedQueryID(t *testing.T) {
	attempts := 0
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return response(http.StatusNotFound, `not found`), nil
		}
		if !strings.Contains(req.URL.Path, "/fresh-viewer/Viewer") {
			t.Fatalf("refreshed path = %q", req.URL.Path)
		}
		return response(http.StatusOK, `{"data":{"viewer":{"result":{"rest_id":"1","core":{"name":"A","screen_name":"a"}}}}}`), nil
	})
	client.discover = func(context.Context, string, string, string) (string, error) {
		return "fresh-viewer", nil
	}
	if _, err := client.FetchViewer(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	if attempts != 2 || !client.ApplyRefreshedQueryIDs(cfg) || cfg.ViewerQID != "fresh-viewer" {
		t.Fatalf("attempts=%d config=%+v", attempts, cfg)
	}
}

func TestFetchViewerRejectsMissingIdentity(t *testing.T) {
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		return response(http.StatusOK, `{"data":{"viewer":{}}}`), nil
	})
	if _, err := client.FetchViewer(context.Background()); err == nil || !strings.Contains(err.Error(), "no account identity") {
		t.Fatalf("err = %v", err)
	}
}
