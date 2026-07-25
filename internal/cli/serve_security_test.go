package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthHTTPHandlerEnforcesToken(t *testing.T) {
	flags := &serveFlags{authToken: "secret"}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := authHTTPHandler(flags, next)

	cases := []struct {
		name   string
		path   string
		bearer string
		want   int
	}{
		{"wrong token", "/render", "wrong", http.StatusUnauthorized},
		{"missing token", "/render", "", http.StatusUnauthorized},
		{"correct token", "/render", "secret", http.StatusOK},
		{"health exempt", "/health", "", http.StatusOK},
		{"ready exempt", "/ready", "", http.StatusOK},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.bearer != "" {
			req.Header.Set("Authorization", "Bearer "+tc.bearer)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s: got status %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
}

func TestEvictTerminalJobsBoundsMemory(t *testing.T) {
	s := &renderJobStore{jobs: map[string]*serverRenderJob{}}
	// A running job at the front must survive eviction.
	running := &serverRenderJob{ID: "job_running", Status: "running"}
	s.jobs[running.ID] = running
	s.order = append(s.order, running.ID)
	for i := 0; i < maxRetainedJobs+50; i++ {
		id := fmt.Sprintf("job_%d", i)
		s.jobs[id] = &serverRenderJob{ID: id, Status: "succeeded"}
		s.order = append(s.order, id)
	}
	s.evictTerminalLocked()

	if len(s.jobs) > maxRetainedJobs {
		t.Fatalf("jobs map not bounded: %d > %d", len(s.jobs), maxRetainedJobs)
	}
	if len(s.order) != len(s.jobs) {
		t.Fatalf("order (%d) and jobs (%d) out of sync", len(s.order), len(s.jobs))
	}
	if _, ok := s.jobs[running.ID]; !ok {
		t.Fatal("running job must never be evicted")
	}
}
