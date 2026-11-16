// Package webui enthält die gebaute Oberfläche (Next.js Static Export), die in das Panel eingebettet wird.
package webui

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

//go:embed all:dist
var assets embed.FS

// Handler liefert die Oberfläche aus. Unbekannte Seiten bekommen die 404-Seite, Dateien mit Hash im Namen werden
// lange zwischengespeichert, Text (HTML, JS, CSS …) geht gzip-komprimiert raus, wenn der Browser das kann.
func Handler() http.Handler {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return handlerFor(root)
}

func handlerFor(root fs.FS) http.Handler {
	files := http.FileServerFS(root)
	packed := &gzipCache{root: root}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		candidates := []string{name, name + ".html", path.Join(name, "index.html")}
		for _, candidate := range candidates {
			info, err := fs.Stat(root, candidate)
			if err != nil || info.IsDir() {
				continue
			}
			if strings.HasPrefix(candidate, "_next/static/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			if packed.serve(w, r, candidate) {
				return
			}
			if candidate == name {
				files.ServeHTTP(w, r)
				return
			}
			serve(w, r, root, candidate)
			return
		}
		// SPA-Fallback für saubere Pfade wie /project/<id>/..., /server/<id>/..., /status/<slug>/... und /nodes/<id>/...
		first := name
		if idx := strings.IndexByte(name, '/'); idx != -1 {
			first = name[:idx]
		}
		if first == "project" || first == "server" || first == "status" || first == "nodes" {
			target := path.Join(first, "index.html")
			if strings.HasSuffix(name, ".txt") {
				target = path.Join(first, "index.txt")
			}
			if info, err := fs.Stat(root, target); err == nil && !info.IsDir() {
				w.Header().Set("Cache-Control", "no-cache")
				if !packed.serve(w, r, target) {
					serve(w, r, root, target)
				}
				return
			}
		}
		if page, err := fs.ReadFile(root, "404.html"); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(page)
			return
		}
		http.NotFound(w, r)
	})
}

func serve(w http.ResponseWriter, r *http.Request, root fs.FS, name string) {
	data, err := fs.ReadFile(root, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType(name))
	_, _ = w.Write(data)
}

// contentType bestimmt den Typ nach der Endung. Die .txt-Dateien sind die Seitendaten von Next.js und müssen als Text
// ankommen, nicht als HTML.
func contentType(name string) string {
	ext := path.Ext(name)
	if ext == ".html" {
		return "text/html; charset=utf-8"
	}
	if value := mime.TypeByExtension(ext); value != "" {
		return value
	}
	return "text/plain; charset=utf-8"
}

// ---------------------------------------------------------------- Kompression

// Ab dieser Größe lohnt sich gzip; kleinere Dateien gehen unverändert raus.
const minGzipSize = 1024

var compressibleTypes = map[string]bool{".html": true, ".js": true, ".css": true, ".json": true, ".svg": true, ".txt": true, ".map": true, ".webmanifest": true}

func compressible(name string) bool {
	return compressibleTypes[strings.ToLower(path.Ext(name))]
}

// acceptsGzip prüft Accept-Encoding auf "gzip" (oder "*"), ohne q=0.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		fields := strings.Split(part, ";")
		coding := strings.ToLower(strings.TrimSpace(fields[0]))
		if coding != "gzip" && coding != "*" {
			continue
		}
		refused := false
		for _, param := range fields[1:] {
			param = strings.TrimSpace(param)
			if value, ok := strings.CutPrefix(param, "q="); ok {
				if q, err := strconv.ParseFloat(value, 64); err == nil && q == 0 {
					refused = true
				}
			}
		}
		if !refused {
			return true
		}
	}
	return false
}

// gzipCache komprimiert jede Datei höchstens einmal. Die eingebetteten Dateien ändern sich zur Laufzeit nie, darum
// darf das Ergebnis bis zum Neustart im Speicher bleiben.
type gzipCache struct {
	root  fs.FS
	files sync.Map // Name -> []byte, nil heißt: lohnt sich nicht
}

func (c *gzipCache) get(name string) ([]byte, bool) {
	if cached, ok := c.files.Load(name); ok {
		data := cached.([]byte)
		return data, data != nil
	}
	data, err := fs.ReadFile(c.root, name)
	if err != nil {
		return nil, false
	}
	var packed []byte
	if len(data) >= minGzipSize {
		var buffer bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
		if _, err := writer.Write(data); err == nil && writer.Close() == nil && buffer.Len() < len(data) {
			packed = buffer.Bytes()
		}
	}
	c.files.Store(name, packed)
	return packed, packed != nil
}

// serve schickt die Datei gepackt, wenn sie komprimierbar ist und der Browser gzip annimmt. Sonst passiert nichts und
// der Aufrufer liefert sie selbst aus.
func (c *gzipCache) serve(w http.ResponseWriter, r *http.Request, name string) bool {
	if !compressible(name) {
		return false
	}
	w.Header().Add("Vary", "Accept-Encoding")
	if !acceptsGzip(r) || r.Header.Get("Range") != "" {
		return false
	}
	data, ok := c.get(name)
	if !ok {
		return false
	}
	header := w.Header()
	header.Set("Content-Type", contentType(name))
	header.Set("Content-Encoding", "gzip")
	header.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
	return true
}
