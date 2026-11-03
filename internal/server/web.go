package server

import (
	"embed"
	"log/slog"
	"net/http"
)

//go:embed web/index.html web/app.css web/app.mjs
var webFiles embed.FS

func addWebRoutes(mux *http.ServeMux, logger *slog.Logger) {
	for _, asset := range []struct {
		path, file, contentType string
	}{
		{"/{$}", "web/index.html", "text/html; charset=utf-8"},
		{"/app.css", "web/app.css", "text/css; charset=utf-8"},
		{"/app.mjs", "web/app.mjs", "text/javascript; charset=utf-8"},
	} {
		mux.HandleFunc("GET "+asset.path, func(w http.ResponseWriter, _ *http.Request) {
			data, err := webFiles.ReadFile(asset.file)
			if err != nil {
				logger.Error("read embedded web asset", "path", asset.path, "error", err)
				http.Error(w, "web asset unavailable", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", asset.contentType)
			w.Header().Set("Cache-Control", "no-store")
			if _, err := w.Write(data); err != nil {
				logger.Debug("write web asset", "path", asset.path, "error", err)
			}
		})
	}
}
