// Package testutil is just the loopback fixture server for the e2e tests.
// serves the same pages the old detection_smoke_test.mjs used.
package testutil

import (
	"fmt"
	"net"
	"net/http"
)

const pageHTML = `<!doctype html>
<button id="click">click</button>
<iframe src="/frame"></iframe>
<script>
  window.clicks = 0;
  document.querySelector('#click').addEventListener('click', () => ++window.clicks);
</script>`

const frameHTML = `<!doctype html><p>frame</p>`

// Custom-test fixtures mirroring utils/custom_tests/*.spec.ts routes.
const contextHTML = `<!doctype html><button id="target">target</button><iframe src="/patchright-context-frame.html"></iframe><script>window.mainMarker = "main"</script>`
const contextFrameHTML = `<!doctype html><script>window.frameMarker = "frame-main"</script>`
const initHTML = `<!doctype html><meta charset="utf-8"><title>init</title><main>document</main>`
const bindingHTML = `<!doctype html><iframe src="/patchright-binding-frame.html"></iframe>`
const bindingFrameHTML = `<!doctype html><title>binding frame</title>`

// Server is a loopback fixture server.
type Server struct {
	HTTP   *http.Server
	Origin string
}

// StartTestServer serves pageHTML at / and frameHTML at /frame on 127.0.0.1,
// plus the custom-test fixtures.
func StartTestServer() (*Server, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/frame":
			fmt.Fprint(w, frameHTML)
		case "/patchright-context.html":
			fmt.Fprint(w, contextHTML)
		case "/patchright-context-frame.html":
			fmt.Fprint(w, contextFrameHTML)
		case "/patchright-init-redirect.html":
			http.Redirect(w, r, "/patchright-init.html", http.StatusFound)
		case "/patchright-init.html":
			fmt.Fprint(w, initHTML)
		case "/patchright-binding.html":
			fmt.Fprint(w, bindingHTML)
		case "/patchright-binding-frame.html":
			fmt.Fprint(w, bindingFrameHTML)
		default:
			fmt.Fprint(w, pageHTML)
		}
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln) //nolint:errcheck
	return &Server{HTTP: srv, Origin: "http://" + ln.Addr().String()}, nil
}

// Close shuts the server down.
func (s *Server) Close() error {
	return s.HTTP.Close()
}
