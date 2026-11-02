package webui

import (
	"net/http"
	"net/http/httptest"
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
