package webui

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCleanURLsRouting(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html":         &fstest.MapFile{Data: []byte("<h1>Home</h1>")},
		"404.html":           &fstest.MapFile{Data: []byte("<h1>Not Found</h1>")},
		"nodes/index.html":   &fstest.MapFile{Data: []byte("<h1>Nodes</h1>")},
		"project/index.html": &fstest.MapFile{Data: []byte("<h1>Project</h1>")},
		"project/index.txt":  &fstest.MapFile{Data: []byte("RSC Project")},
		"server/index.html":  &fstest.MapFile{Data: []byte("<h1>Server</h1>")},
		"server/index.txt":   &fstest.MapFile{Data: []byte("RSC Server")},
		"status/index.html":  &fstest.MapFile{Data: []byte("<h1>Status</h1>")},
		"status/index.txt":   &fstest.MapFile{Data: []byte("RSC Status")},
	}

	handler := handlerFor(mockFS)

	tests := []struct {
		url          string
		expectedCode int
		expectedBody string
	}{
		{"/", http.StatusOK, "<h1>Home</h1>"},
		{"/nodes/", http.StatusOK, "<h1>Nodes</h1>"},
		{"/project/p1/", http.StatusOK, "<h1>Project</h1>"},
		{"/project/p1/schedules/", http.StatusOK, "<h1>Project</h1>"},
		{"/project/p1/index.txt", http.StatusOK, "RSC Project"},
		{"/server/s1/", http.StatusOK, "<h1>Server</h1>"},
		{"/server/s1/console/", http.StatusOK, "<h1>Server</h1>"},
		{"/server/s1/index.txt", http.StatusOK, "RSC Server"},
		{"/status/demo/", http.StatusOK, "<h1>Status</h1>"},
		{"/status/demo/index.txt", http.StatusOK, "RSC Status"},
		{"/nodes/sub/", http.StatusOK, "<h1>Nodes</h1>"},
		{"/nodes/n1/", http.StatusOK, "<h1>Nodes</h1>"},
		{"/unknown/path/", http.StatusNotFound, "<h1>Not Found</h1>"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedCode {
				t.Fatalf("Für %s Status %d erwartet, bekam %d", tt.url, tt.expectedCode, rec.Code)
			}
			if rec.Body.String() != tt.expectedBody {
				t.Fatalf("Für %s Inhalt %q erwartet, bekam %q", tt.url, tt.expectedBody, rec.Body.String())
			}
		})
	}
}

func testRoot() fstest.MapFS {
	script := strings.Repeat("console.log('lodestone');\n", 200)
	return fstest.MapFS{
		"index.html":               {Data: []byte("<html>" + strings.Repeat("Übersicht ", 300) + "</html>")},
		"server/index.html":        {Data: []byte("<html>server</html>")},
		"project/index.html":       {Data: []byte("<html>" + strings.Repeat("Projekt ", 300) + "</html>")},
		"project/index.txt":        {Data: []byte(strings.Repeat("1:RSC-Daten\n", 200))},
		"_next/static/chunks/a.js": {Data: []byte(script)},
		"logo.png":                 {Data: bytes.Repeat([]byte{0x89}, 4096)},
		"404.html":                 {Data: []byte("nicht gefunden")},
	}
}

func get(t *testing.T, handler http.Handler, target, encoding string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	if encoding != "" {
		request.Header.Set("Accept-Encoding", encoding)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func gunzip(t *testing.T, data []byte) string {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("keine gültigen gzip-Daten: %v", err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(plain)
}

func TestGzipForScripts(t *testing.T) {
	root := testRoot()
	handler := handlerFor(root)
	for range 2 { // der zweite Aufruf kommt aus dem Zwischenspeicher
		response := get(t, handler, "/_next/static/chunks/a.js", "br, gzip")
		if response.Code != http.StatusOK || response.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("Status %d, Encoding %q", response.Code, response.Header().Get("Content-Encoding"))
		}
		if !strings.HasPrefix(response.Header().Get("Content-Type"), "text/javascript") {
			t.Fatalf("Content-Type %q", response.Header().Get("Content-Type"))
		}
		if response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || response.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatalf("Header %v", response.Header())
		}
		if gunzip(t, response.Body.Bytes()) != string(root["_next/static/chunks/a.js"].Data) {
			t.Fatal("entpackter Inhalt weicht ab")
		}
	}
}

func TestGzipForPages(t *testing.T) {
	root := testRoot()
	response := get(t, handlerFor(root), "/", "gzip")
	if response.Header().Get("Content-Encoding") != "gzip" || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("Header %v", response.Header())
	}
	if gunzip(t, response.Body.Bytes()) != string(root["index.html"].Data) {
		t.Fatal("entpackter Inhalt weicht ab")
	}
}

func TestNoGzipWhenNotWanted(t *testing.T) {
	handler := handlerFor(testRoot())
	cases := map[string]struct{ target, encoding string }{
		"ohne Accept-Encoding": {"/_next/static/chunks/a.js", ""},
		"gzip abgelehnt":       {"/_next/static/chunks/a.js", "gzip;q=0, br"},
		"kleine Datei":         {"/server/", "gzip"},
		"Bild":                 {"/logo.png", "gzip"},
	}
	for label, c := range cases {
		response := get(t, handler, c.target, c.encoding)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: Status %d", label, response.Code)
		}
		if response.Header().Get("Content-Encoding") != "" {
			t.Fatalf("%s: unerwartet komprimiert", label)
		}
	}
}

func TestNotFoundPage(t *testing.T) {
	response := get(t, handlerFor(testRoot()), "/gibt-es-nicht/", "gzip")
	if response.Code != http.StatusNotFound || response.Body.String() != "nicht gefunden" {
		t.Fatalf("Status %d, Inhalt %q", response.Code, response.Body.String())
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := map[string]bool{"": false, "gzip": true, "GZIP": true, "br, gzip;q=0.5": true, "gzip;q=0": false, "*": true, "deflate, br": false, "gzip; q=0.0": false}
	for header, want := range cases {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Accept-Encoding", header)
		if got := acceptsGzip(request); got != want {
			t.Errorf("%q: %v, erwartet %v", header, got, want)
		}
	}
}

func TestGzipForCleanURLs(t *testing.T) {
	root := testRoot()
	handler := handlerFor(root)
	page := get(t, handler, "/project/p1/schedules/", "gzip")
	if page.Header().Get("Content-Encoding") != "gzip" || page.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("Seite: Header %v", page.Header())
	}
	if gunzip(t, page.Body.Bytes()) != string(root["project/index.html"].Data) {
		t.Fatal("Seite: entpackter Inhalt weicht ab")
	}
	data := get(t, handler, "/project/p1/index.txt", "gzip")
	if data.Header().Get("Content-Encoding") != "gzip" || !strings.HasPrefix(data.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("Seitendaten: Header %v", data.Header())
	}
	plain := get(t, handler, "/project/p1/index.txt", "")
	if !strings.HasPrefix(plain.Header().Get("Content-Type"), "text/plain") || plain.Body.String() != string(root["project/index.txt"].Data) {
		t.Fatalf("Seitendaten ohne gzip: Typ %q", plain.Header().Get("Content-Type"))
	}
}
