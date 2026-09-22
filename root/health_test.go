package root

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveAllowsConfiguredClientOrigin(t *testing.T) {
	app := &app{cfg: Config{BaseURL: "http://localhost:7010"}}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:7010")
	res := httptest.NewRecorder()

	app.live(res, req)

	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:7010" {
		t.Fatalf("allow-origin = %q, want configured client origin", got)
	}
	if got := res.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("vary = %q, want Origin", got)
	}
}

func TestLiveDoesNotAllowUnexpectedOrigin(t *testing.T) {
	app := &app{cfg: Config{BaseURL: "http://localhost:7010"}}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://unexpected.example")
	res := httptest.NewRecorder()

	app.live(res, req)

	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow-origin = %q, want no cross-origin permission", got)
	}
}
