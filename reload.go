package antedom

import (
	"bytes"
	"hash/fnv"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ReloadPath is where LiveReload serves its server-sent-events
// stream. The injected script connects here, so LiveReload must wrap
// the outermost handler (outside any http.StripPrefix).
const ReloadPath = "/.antedom/reload"

// reloadScript reconnects after a dropped stream (server restart)
// and reloads then too, so a rebuilt binary refreshes the page.
const reloadScript = `<script>(()=>{const es=new EventSource("` + ReloadPath + `");let lost=false;es.onmessage=()=>location.reload();es.onerror=()=>lost=true;es.onopen=()=>{if(lost)location.reload()}})()</script>` + "\n"

// LiveReload wraps next for development serving: browsers reload when
// anything under dirs changes. It serves an event stream at ReloadPath
// that fires when the tree's fingerprint changes (polled twice a
// second), and appends a script listening on that stream to every
// HTML response.
func LiveReload(next http.Handler, dirs ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ReloadPath {
			serveReload(w, r, dirs)
			return
		}
		rec := &responseBuffer{header: http.Header{}, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		body := rec.buf.Bytes()
		if strings.HasPrefix(rec.header.Get("Content-Type"), "text/html") {
			body = append(body, reloadScript...)
			// The appended script invalidates any length ServeFile set.
			rec.header.Del("Content-Length")
		}
		for k, v := range rec.header {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.status)
		w.Write(body)
	})
}

func serveReload(w http.ResponseWriter, r *http.Request, dirs []string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, ": watching\n\n")
	fl.Flush()
	last := fingerprint(dirs)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if fp := fingerprint(dirs); fp != last {
				last = fp
				io.WriteString(w, "data: reload\n\n")
				fl.Flush()
			}
		}
	}
}

// fingerprint hashes every file's path, size, and mtime under dirs.
// Walk errors are skipped: editors rename and remove files mid-save,
// and the next poll sees the settled state.
func fingerprint(dirs []string) uint64 {
	h := fnv.New64a()
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			io.WriteString(h, p)
			h.Write([]byte{0})
			io.WriteString(h, info.ModTime().String())
			h.Write([]byte{0})
			io.WriteString(h, strconv.FormatInt(info.Size(), 10))
			h.Write([]byte{0})
			return nil
		})
	}
	return h.Sum64()
}

// responseBuffer captures a response so LiveReload can append to HTML
// bodies after the handler finishes. Buffering whole responses is
// fine for a development server.
type responseBuffer struct {
	header http.Header
	buf    bytes.Buffer
	status int
}

func (r *responseBuffer) Header() http.Header         { return r.header }
func (r *responseBuffer) Write(p []byte) (int, error) { return r.buf.Write(p) }
func (r *responseBuffer) WriteHeader(status int)      { r.status = status }
