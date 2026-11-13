package main

import (
	"net/http"
	"os"
	"path/filepath"
)

func webPage(w http.ResponseWriter, r *http.Request) {
	index := filepath.Join(webRoot(), "index.html")
	if _, err := os.Stat(index); err != nil {
		http.Error(w, "Интерфейс Бобби не собран. Запустите npm --prefix frontend run build.", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, index)
}

func webRoot() string {
	return env("BOBBY_WEB_ROOT", "frontend/dist")
}
