package spa

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<html>index</html>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
		"favicon.ico":   {Data: []byte("icon-bytes")},
	}
}

func TestHandler_ServesExistingAsset(t *testing.T) {
	h, err := New(testFS())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "console.log('app')" {
		t.Fatalf("expected asset content, got %q", got)
	}
}

func TestHandler_FallsBackToIndexForClientRoute(t *testing.T) {
	h, err := New(testFS())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/week", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "<html>index</html>" {
		t.Fatalf("expected index.html content, got %q", got)
	}
}

func TestHandler_ServesIndexAtRoot(t *testing.T) {
	h, err := New(testFS())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "<html>index</html>" {
		t.Fatalf("expected index.html content, got %q", got)
	}
}

func TestHandler_FallsBackForNestedClientRoute(t *testing.T) {
	h, err := New(testFS())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/calendar/week/2026-01-01", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "<html>index</html>" {
		t.Fatalf("expected index.html content, got %q", got)
	}
}

func TestNew_MissingIndexHTML(t *testing.T) {
	_, err := New(fstest.MapFS{"assets/app.js": {Data: []byte("x")}})
	if err == nil {
		t.Fatalf("expected an error when index.html is missing")
	}
}
