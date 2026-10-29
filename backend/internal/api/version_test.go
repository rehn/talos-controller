package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionEndpointIsPublic(t *testing.T) {
	s := unconfiguredServer(t)
	s.Version = "v1.2.3"

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["version"] != "v1.2.3" {
		t.Fatalf("body = %s (%v)", rec.Body, err)
	}
}
