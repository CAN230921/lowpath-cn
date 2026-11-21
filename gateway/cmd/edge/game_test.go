package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGameEvent(t *testing.T) {
	mux := http.NewServeMux()
	mountGame(mux)
	for _, test := range []struct {
		name   string
		query  string
		status int
		event  string
	}{
		{"bell", "from=3&to=4", http.StatusOK, "bell"},
		{"ferry", "from=1&to=9", http.StatusOK, "ferry"},
		{"rain", "from=22&to=14", http.StatusOK, "rain"},
		{"quiet", "from=0&to=1", http.StatusNoContent, ""},
		{"diagonal", "from=0&to=9", http.StatusBadRequest, ""},
		{"row wrap", "from=7&to=8", http.StatusBadRequest, ""},
		{"outside board", "from=63&to=64", http.StatusBadRequest, ""},
		{"missing cell", "from=0", http.StatusBadRequest, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/game/event?"+test.query, nil)
			result := httptest.NewRecorder()
			mux.ServeHTTP(result, req)
			if result.Code != test.status || result.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected game event response: %d, %v", result.Code, result.Header())
			}
			if test.event != "" && !strings.Contains(result.Body.String(), `"event":"`+test.event+`"`) {
				t.Errorf("expected event %q in %q", test.event, result.Body.String())
			}
			if test.event == "" && result.Body.Len() != 0 {
				t.Errorf("expected empty response, got %q", result.Body.String())
			}
		})
	}
}
