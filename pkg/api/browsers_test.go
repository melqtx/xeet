package api

import (
	"reflect"
	"testing"
)

func TestSupportedBrowsers(t *testing.T) {
	want := []string{"Chrome", "Helium", "Firefox", "Brave", "Zen"}
	if got := SupportedBrowsers(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedBrowsers() = %v, want %v", got, want)
	}
}
