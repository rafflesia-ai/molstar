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
	"testing"
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
