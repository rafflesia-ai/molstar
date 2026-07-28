package job

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrepareRuntimeOfflineCacheMissFails(t *testing.T) {
	j := Job{
		Version: 1,
		Runtime: Runtime{
			Cache:   t.TempDir(),
			Offline: true,
		},
		Inputs: map[string]Input{
			"protein": {ID: "1cbs", Provider: "pdbe"},
		},
		Scene: Scene{Structures: []Structure{{Source: "protein"}}},
	}
	if _, _, err := PrepareRuntime(context.Background(), j); err == nil {
		t.Fatal("expected offline cache miss to fail")
	}
}

func TestPrepareRuntimeOfflineCacheHitRewritesInput(t *testing.T) {
	cacheDir := t.TempDir()
	input := Input{ID: "1cbs", Provider: "pdbe"}
	resolved, err := input.ResolvedURL()
	if err != nil {
		t.Fatal(err)
	}
	cachePath := cachePathFor(cacheDir, resolved, input.ResolvedFormat())
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}
	j := Job{
		Version: 1,
		Runtime: Runtime{
			Cache:   cacheDir,
			Offline: true,
		},
		Inputs: map[string]Input{
			"protein": input,
		},
		Scene: Scene{Structures: []Structure{{Source: "protein"}}},
	}
	prepared, report, err := PrepareRuntime(context.Background(), j)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Inputs["protein"].Path != cachePath {
		t.Fatalf("expected cached path %q, got %q", cachePath, prepared.Inputs["protein"].Path)
	}
	if len(report.CachedInputs) != 1 || !report.CachedInputs[0].Cached {
		t.Fatalf("unexpected cache report: %#v", report.CachedInputs)
	}
}

func TestPrepareRuntimeDownloadsAndListsCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("cached-data"))
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()
	j := Job{
		Version: 1,
		Runtime: Runtime{
			Cache:      cacheDir,
			AllowHosts: []string{parsed.Hostname()},
		},
		Inputs: map[string]Input{
			"protein": {URL: server.URL + "/model.cif", Format: "mmcif"},
		},
		Scene: Scene{Structures: []Structure{{Source: "protein"}}},
		Outputs: []Output{{
			Type: "mvsj",
			Path: filepath.Join(t.TempDir(), "scene.mvsj"),
		}},
	}
	prepared, report, err := PrepareRuntime(context.Background(), j)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Inputs["protein"].Path == "" {
		t.Fatalf("expected cached path in prepared input: %#v", prepared.Inputs["protein"])
	}
	if len(report.CachedInputs) != 1 || report.CachedInputs[0].Cached {
		t.Fatalf("expected cache miss/download report, got %#v", report.CachedInputs)
	}
	entries, err := ListCache(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one cache entry, got %#v", entries)
	}
	if !entries[0].Verified || entries[0].Bytes != int64(len("cached-data")) {
		t.Fatalf("unexpected cache metadata: %#v", entries[0])
	}
}

func TestPrepareRuntimeRedirectCannotBypassAllowHosts(t *testing.T) {
	// An allowed host that redirects the fetch to an internal metadata address.
	// The redirect target is not in AllowHosts, so it must be refused per-hop.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	j := Job{
		Version: 1,
		Runtime: Runtime{Cache: t.TempDir(), AllowHosts: []string{parsed.Hostname()}},
		Inputs:  map[string]Input{"protein": {URL: server.URL + "/model.cif", Format: "mmcif"}},
		Scene:   Scene{Structures: []Structure{{Source: "protein"}}},
		Outputs: []Output{{Type: "mvsj", Path: filepath.Join(t.TempDir(), "scene.mvsj")}},
	}
	_, _, err = PrepareRuntime(context.Background(), j)
	if err == nil {
		t.Fatal("expected redirect to a disallowed host to be rejected")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected host allowlist error, got: %v", err)
	}
}

func TestPrepareRuntimeConfinesOutputToAllowedPaths(t *testing.T) {
	allowed := t.TempDir()
	input := filepath.Join(allowed, "model.cif")
	if err := os.WriteFile(input, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	newJob := func(outputPath string) Job {
		return Job{
			Version: 1,
			Runtime: Runtime{AllowPaths: []string{allowed}},
			Inputs:  map[string]Input{"protein": {Path: input, Format: "mmcif"}},
			Scene:   Scene{Structures: []Structure{{Source: "protein"}}},
			Outputs: []Output{{Type: "mvsj", Path: outputPath}},
		}
	}

	if _, _, err := PrepareRuntime(context.Background(), newJob(filepath.Join(t.TempDir(), "escape.mvsj"))); err == nil {
		t.Fatal("expected output outside allowed roots to be rejected")
	} else if !strings.Contains(err.Error(), "outside allowed roots") {
		t.Fatalf("expected allow-path error, got: %v", err)
	}

	if _, _, err := PrepareRuntime(context.Background(), newJob(filepath.Join(allowed, "scene.mvsj"))); err != nil {
		t.Fatalf("output inside allowed roots should be accepted, got: %v", err)
	}
}

func TestListCacheIgnoresHiddenAndAppleDoubleFiles(t *testing.T) {
	cacheDir := t.TempDir()
	downloads := filepath.Join(cacheDir, "downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real hex-named cache file.
	real := filepath.Join(downloads, "abc123.bcif")
	if err := os.WriteFile(real, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Noise a non-native filesystem (exFAT/SMB) or macOS leaves behind.
	for _, noise := range []string{"._abc123.bcif", ".DS_Store", "._.hidden"} {
		if err := os.WriteFile(filepath.Join(downloads, noise), []byte("junk"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := ListCache(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one real cache entry, got %d: %#v", len(entries), entries)
	}
	if filepath.Base(entries[0].Path) != "abc123.bcif" {
		t.Fatalf("unexpected entry: %#v", entries[0])
	}
}

func TestPrepareRuntimeBlocksDisallowedHost(t *testing.T) {
	j := Job{
		Version: 1,
		Runtime: Runtime{
			Cache:      t.TempDir(),
			AllowHosts: []string{"example.org"},
		},
		Inputs: map[string]Input{
			"protein": {URL: "https://example.com/model.cif"},
		},
		Scene: Scene{Structures: []Structure{{Source: "protein"}}},
	}
	if _, _, err := PrepareRuntime(context.Background(), j); err == nil {
		t.Fatal("expected disallowed host to fail")
	}
}

func TestJSONSchemaCanMarshal(t *testing.T) {
	data, err := json.Marshal(JSONSchema())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("schema is empty")
	}
}

// max_atoms cannot be checked for BinaryCIF and trajectory formats without a
// full parser, and BinaryCIF is what every pdbe/rcsb identifier resolves to. The
// limit therefore did not bind on the most common input path, silently, so an
// operator running the `locked` profile believed they were protected when they
// were not. The skip is now reported.
func TestAtomLimitReportsWhenItCannotBeEnforced(t *testing.T) {
	dir := t.TempDir()
	pdbPath := filepath.Join(dir, "two.pdb")
	pdb := "ATOM      1  CA  ALA A   1      11.000  12.000  13.000  1.00  0.00           C\n" +
		"ATOM      2  CB  ALA A   1      12.000  13.000  14.000  1.00  0.00           C\n"
	if err := os.WriteFile(pdbPath, []byte(pdb), 0o644); err != nil {
		t.Fatal(err)
	}
	bcifPath := filepath.Join(dir, "model.bcif")
	if err := os.WriteFile(bcifPath, []byte("not really bcif"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("countable format still enforces", func(t *testing.T) {
		j := Job{
			Version: 1,
			Runtime: Runtime{Network: boolPtr(false), MaxAtoms: 1, AllowPaths: []string{dir}},
			Inputs:  map[string]Input{"p": {Path: pdbPath, Format: "pdb"}},
		}
		if _, _, err := PrepareRuntime(context.Background(), j); err == nil {
			t.Fatal("expected the atom limit to reject a 2-atom structure with max_atoms=1")
		}
	})

	t.Run("uncountable format warns instead of silently passing", func(t *testing.T) {
		j := Job{
			Version: 1,
			Runtime: Runtime{Network: boolPtr(false), MaxAtoms: 1, AllowPaths: []string{dir}},
			Inputs:  map[string]Input{"p": {Path: bcifPath, Format: "bcif"}},
		}
		_, report, err := PrepareRuntime(context.Background(), j)
		if err != nil {
			t.Fatalf("bcif input should not fail preparation: %v", err)
		}
		if len(report.Warnings) != 1 {
			t.Fatalf("expected one skipped-limit warning, got %#v", report.Warnings)
		}
		if !strings.Contains(report.Warnings[0], "max_atoms") || !strings.Contains(report.Warnings[0], "bcif") {
			t.Fatalf("warning should name the limit and the format: %q", report.Warnings[0])
		}
	})

	t.Run("no limit set produces no warning", func(t *testing.T) {
		j := Job{
			Version: 1,
			Runtime: Runtime{Network: boolPtr(false), AllowPaths: []string{dir}},
			Inputs:  map[string]Input{"p": {Path: bcifPath, Format: "bcif"}},
		}
		_, report, err := PrepareRuntime(context.Background(), j)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Warnings) != 0 {
			t.Fatalf("expected no warnings without max_atoms, got %#v", report.Warnings)
		}
	})
}

func boolPtr(v bool) *bool { return &v }

// The download temp file used to be a fixed "<cachePath>.tmp" derived from the
// URL, so every concurrent fetch of the same input shared one file: they
// overwrote each other and all but one rename failed with ENOENT. `batch
// --concurrency` failed most of its jobs whenever they shared an uncached input.
func TestConcurrentDownloadsOfTheSameInputAllSucceed(t *testing.T) {
	payload := []byte(strings.Repeat("data_test\n", 4096))
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// Widen the window between create and rename.
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "chemical/x-cif")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	const workers = 8
	errs := make(chan error, workers)
	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			j := Job{
				Version: 1,
				Runtime: Runtime{Cache: cacheDir},
				Inputs:  map[string]Input{"p": {URL: server.URL + "/model.cif", Format: "mmcif"}},
			}
			_, _, err := PrepareRuntime(context.Background(), j)
			errs <- err
		}()
	}
	start.Done()
	done.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent prepare failed: %v", err)
		}
	}

	cachePath := CachePathFor(cacheDir, server.URL+"/model.cif", "mmcif")
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("cache entry missing: %v", err)
	}
	if info.Size() != int64(len(payload)) {
		t.Fatalf("cache entry is %d bytes, want %d", info.Size(), len(payload))
	}
	// No temp files may survive.
	entries, err := os.ReadDir(filepath.Join(cacheDir, "downloads"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("leftover temp file %q", entry.Name())
		}
	}
}

// A cache entry whose size no longer matches what was recorded is damaged.
// Handing it to the renderer produced an opaque "renderer failed" that looked
// retryable, so an offline caller retried a broken file forever.
func TestCorruptCacheEntryIsDetected(t *testing.T) {
	payload := []byte(strings.Repeat("data_test\n", 512))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	newJob := func(offline bool) Job {
		runtime := Runtime{Cache: cacheDir}
		if offline {
			runtime.Offline = true
		}
		return Job{
			Version: 1,
			Runtime: runtime,
			Inputs:  map[string]Input{"p": {URL: server.URL + "/model.cif", Format: "mmcif"}},
		}
	}

	if _, _, err := PrepareRuntime(context.Background(), newJob(false)); err != nil {
		t.Fatalf("warm the cache: %v", err)
	}
	cachePath := CachePathFor(cacheDir, server.URL+"/model.cif", "mmcif")
	truncate := func() {
		if err := os.WriteFile(cachePath, payload[:64], 0o644); err != nil {
			t.Fatal(err)
		}
	}

	truncate()
	_, _, err := PrepareRuntime(context.Background(), newJob(true))
	if err == nil {
		t.Fatal("an offline run must reject a corrupt cache entry instead of rendering it")
	}
	if !strings.Contains(err.Error(), "corrupt") {
		t.Fatalf("error should name the corruption: %v", err)
	}

	truncate()
	_, report, err := PrepareRuntime(context.Background(), newJob(false))
	if err != nil {
		t.Fatalf("an online run should re-download a corrupt entry: %v", err)
	}
	if len(report.Warnings) == 0 || !strings.Contains(report.Warnings[0], "corrupt") {
		t.Fatalf("re-download should be reported: %#v", report.Warnings)
	}
	info, err := os.Stat(cachePath)
	if err != nil || info.Size() != int64(len(payload)) {
		t.Fatalf("cache entry was not repaired: %v %d", err, info.Size())
	}

	// A healthy entry produces no warning and works offline.
	_, report, err = PrepareRuntime(context.Background(), newJob(true))
	if err != nil {
		t.Fatalf("healthy cache should serve an offline run: %v", err)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("healthy cache should not warn: %#v", report.Warnings)
	}
}
