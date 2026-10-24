package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGuard(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := guard(8080, ok)

	cases := []struct {
		name    string
		method  string
		host    string
		headers map[string]string
		want    int
	}{
		{"same-origin GET", "GET", "localhost:8080", nil, 200},
		{"rebinding host", "GET", "evil.com:8080", nil, 403},
		{"other localhost port DELETE", "DELETE", "localhost:8080", map[string]string{"Origin": "http://localhost:3000"}, 403},
		{"same-origin DELETE", "DELETE", "127.0.0.1:8080", map[string]string{"Origin": "http://127.0.0.1:8080"}, 200},
		{"cross-site fetch metadata", "POST", "localhost:8080", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"preflight foreign origin", "OPTIONS", "localhost:8080", map[string]string{"Origin": "http://localhost:9999"}, 403},
		{"preflight allowed origin", "OPTIONS", "localhost:8080", map[string]string{"Origin": "http://localhost:8080"}, 204},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, "/api/jobs", nil)
			req.Host = c.host
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Fatalf("got %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func TestJobIDValidation(t *testing.T) {
	if uuidRe.MatchString("1; DROP TABLE jobs") {
		t.Fatal("invalid uuid accepted")
	}
	if !uuidRe.MatchString("123e4567-e89b-12d3-a456-426614174000") {
		t.Fatal("valid uuid rejected")
	}
}

func TestListJobColumnsTruncatesDescription(t *testing.T) {
	if !strings.Contains(listJobColumns, "left(COALESCE(description, ''), 2000)") {
		t.Fatalf("liste sorgusu açıklamayı kırpmalı: %s", listJobColumns)
	}
	if !strings.Contains(baseJobColumns, "COALESCE(description, '')") || strings.Contains(baseJobColumns, "left(") {
		t.Fatal("tek ilan sorgusu tam açıklamayı döndürmeli")
	}
}
