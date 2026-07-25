package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestServeConcurrentJobChurnRace hammers the render job store with more
// concurrent async submissions than maxRetainedJobs so terminal-job eviction
// runs under contention, while readers concurrently list/snapshot jobs. Its real
// assertion is the race detector (run with -race); it also checks the in-memory
// job map stays bounded. Heavy, so skipped under -short.
func TestServeConcurrentJobChurnRace(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test; skipped under -short")
	}
	dir := t.TempDir()
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	flags := &serveFlags{noWorker: true, rendererCommand: "false", dryRun: true, workers: 8, queue: 64}
	gate := newRenderGate(flags.workers, flags.queue)
	store := newRenderJobStore(a, flags, gate)
	server := httptest.NewServer(a.serveMux(flags, cmd, gate, store))
	defer server.Close()
	client := server.Client()

	body, err := json.Marshal(minimalServeJob(t, dir))
	if err != nil {
		t.Fatal(err)
	}

	const total = maxRetainedJobs + 250 // exceed the cap to force eviction
	var submitted int64

	// Shared pool of submitted job IDs that readers poll via /jobs/{id},
	// exercising get(s.mu) -> snapshot(j.mu) against evict(s.mu -> j.mu).
	var idMu sync.Mutex
	ids := make([]string, 0, total)
	sampleID := func() string {
		idMu.Lock()
		defer idMu.Unlock()
		if len(ids) == 0 {
			return ""
		}
		return ids[int(atomic.LoadInt64(&submitted))%len(ids)]
	}

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for r := 0; r < 8; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if id := sampleID(); id != "" {
						if resp, err := client.Get(server.URL + "/jobs/" + id); err == nil {
							_, _ = io.Copy(io.Discard, resp.Body)
							_ = resp.Body.Close()
						}
					}
					if resp, err := client.Get(server.URL + "/metrics"); err == nil {
						_, _ = io.Copy(io.Discard, resp.Body)
						_ = resp.Body.Close()
					}
				}
			}
		}()
	}

	var submitters sync.WaitGroup
	sem := make(chan struct{}, 48)
	for i := 0; i < total; i++ {
		submitters.Add(1)
		sem <- struct{}{}
		go func() {
			defer submitters.Done()
			defer func() { <-sem }()
			for attempt := 0; attempt < 500; attempt++ {
				resp, err := client.Post(server.URL+"/render?async=true", "application/json", bytes.NewReader(body))
				if err != nil {
					return
				}
				status := resp.StatusCode
				var decoded struct {
					ID string `json:"id"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&decoded)
				_ = resp.Body.Close()
				if status == http.StatusTooManyRequests {
					time.Sleep(time.Millisecond) // gate saturated; retry
					continue
				}
				if decoded.ID != "" {
					idMu.Lock()
					ids = append(ids, decoded.ID)
					idMu.Unlock()
				}
				atomic.AddInt64(&submitted, 1)
				return
			}
		}()
	}
	submitters.Wait()

	// Drain in-flight async jobs so no background run() goroutine is still
	// writing when the test's temp dir is cleaned up.
	drainDeadline := time.Now().Add(30 * time.Second)
	for gate.active.Load() > 0 {
		if time.Now().After(drainDeadline) {
			t.Fatalf("jobs did not drain: %d still active", gate.active.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}

	close(stop)
	readers.Wait()

	if submitted < maxRetainedJobs {
		t.Fatalf("only %d jobs submitted; too few to exercise eviction", submitted)
	}

	// Eviction must have kept the retained job map bounded (and never leaked a
	// running job). count() reads the live map under the store lock.
	if got := store.count(); got > maxRetainedJobs {
		t.Fatalf("job map not bounded after churn: %d > %d", got, maxRetainedJobs)
	}
}
