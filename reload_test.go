package antedom

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveReloadInjection(t *testing.T) {
	s := demoSite(t)
	h := LiveReload(s.Handler(), s.Pages)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	body, _ := io.ReadAll(rec.Result().Body)
	if !strings.Contains(string(body), ReloadPath) {
		t.Errorf("HTML response missing reload script: %s", body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/sub/plain.txt", nil))
	body, _ = io.ReadAll(rec.Result().Body)
	if got := string(body); got != "passthrough" {
		t.Errorf("non-HTML response altered: %q", got)
	}
}

func TestLiveReloadStream(t *testing.T) {
	s := demoSite(t)
	srv := httptest.NewServer(LiveReload(s.Handler(), s.Pages))
	defer srv.Close()
	resp, err := http.Get(srv.URL + ReloadPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q, want text/event-stream", ct)
	}
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	if l := <-lines; l != ": watching" {
		t.Fatalf("greeting %q, want %q", l, ": watching")
	}
	// Give the watcher a poll to record its baseline, then change a file.
	time.Sleep(600 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(s.Pages, "new.html"), []byte("<p>hi</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("stream closed without reload event")
			}
			if l == "data: reload" {
				return
			}
		case <-deadline:
			t.Fatal("no reload event within 5s of file change")
		}
	}
}
