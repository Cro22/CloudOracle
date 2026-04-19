package api

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var embeddedDist embed.FS

const notBuiltHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <title>CloudOracle · Dashboard not built</title>
  <style>
    body { font-family: Inter, system-ui, sans-serif; max-width: 640px; margin: 80px auto; padding: 0 24px; color: #0f172a; }
    h1 { margin: 0 0 12px; font-size: 28px; }
    p { line-height: 1.6; color: #475569; }
    pre { background: #f1f5f9; padding: 14px 16px; border-radius: 8px; font-size: 14px; }
    code { background: #f1f5f9; padding: 2px 6px; border-radius: 4px; }
    a { color: #2563eb; }
  </style>
</head>
<body>
  <h1>Dashboard bundle not found</h1>
  <p>The Go binary embeds the React dashboard, but the frontend hasn't been built yet.</p>
  <pre>cd web
npm install   # first time only
npm run build</pre>
  <p>Then rebuild the binary (<code>go build</code>) and restart the server. The JSON API is still available at <a href="/api/summary">/api/*</a>.</p>
</body>
</html>
`

func distFS() (fs.FS, bool) {
	sub, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return nil, false
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return sub, false
	}
	return sub, true
}

func staticHandler() http.Handler {
	sub, built := distFS()
	if !built {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(notBuiltHTML))
		})
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			fileServer.ServeHTTP(w, r)
			return
		}

		f, err := sub.Open(path)
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		index, err := sub.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer index.Close()

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, index)
	})
}
