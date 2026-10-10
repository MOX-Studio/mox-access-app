package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/MOX-Studio/mox-access-app/internal/selfupdate"
)

func TestUpdateIsToldOnceAndAgainADayLater(t *testing.T) {
	tag := "v0.6.1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": []map[string]any{{"name": selfupdate.AssetName(runtime.GOOS, runtime.GOARCH), "browser_download_url": "http://unused/archive.zip"}}})
	}))
	defer srv.Close()
	var told []string
	u := &Updates{U: &selfupdate.Updater{Current: "0.6.0", API: srv.URL}, Notify: func(_, text string) { told = append(told, text) }}
	check := func() {
		t.Helper()
		if _, err := u.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	check()
	check()
	if len(told) != 1 || u.Available() != "0.6.1" {
		t.Fatalf("hourly checks must not repeat the note: %v", told)
	}
	u.toldAt = time.Now().Add(-remindEvery)
	check()
	if len(told) != 2 {
		t.Fatalf("an update left for a day must be told again: %v", told)
	}
	tag = "v0.6.2"
	check()
	if len(told) != 3 || u.Available() != "0.6.2" {
		t.Fatalf("a newer version must be told at once: %v", told)
	}
}
