package api

import (
	"embed"
	"net/http"
)

//go:embed web/*
var webAssets embed.FS

func registerUI(mux *http.ServeMux) {
	for route, asset := range map[string]struct{ path, contentType string }{
		"GET /test":                 {"web/test.html", "text/html; charset=utf-8"},
		"GET /assets/scenarios.js":  {"web/scenarios.js", "text/javascript; charset=utf-8"},
		"GET /assets/test.js":       {"web/test.js", "text/javascript; charset=utf-8"},
		"GET /assets/scenarios.css": {"web/scenarios.css", "text/css; charset=utf-8"},
		"GET /assets/examples.json": {"web/examples.json", "application/json"},
		"GET /{$}":                  {"web/index.html", "text/html; charset=utf-8"},
		"GET /assets/app.css":       {"web/app.css", "text/css; charset=utf-8"},
		"GET /assets/app.js":        {"web/app.js", "text/javascript; charset=utf-8"},
	} {
		data, err := webAssets.ReadFile(asset.path)
		if err != nil {
			panic(err)
		}
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", asset.contentType)
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
			if r.Method != http.MethodHead {
				_, _ = w.Write(data)
			}
		})
	}
}
