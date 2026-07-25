package job

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const defaultMaxDownloadBytes int64 = 512 * 1024 * 1024
const defaultMaxArchiveBytes int64 = 512 * 1024 * 1024

type RuntimeReport struct {
	CachedInputs []CachedInput `json:"cached_inputs,omitempty"`
}

type CachedInput struct {
	Ref    string `json:"ref"`
	URL    string `json:"url"`
	Path   string `json:"path"`
	Cached bool   `json:"cached"`
	Bytes  int64  `json:"bytes,omitempty"`
}

type CacheEntry struct {
	URL      string    `json:"url"`
	Path     string    `json:"path"`
	Format   string    `json:"format"`
	Bytes    int64     `json:"bytes"`
	SHA256   string    `json:"sha256"`
	CachedAt time.Time `json:"cached_at"`
	Verified bool      `json:"verified,omitempty"`
}

func PrepareRuntime(ctx context.Context, j Job) (Job, RuntimeReport, error) {
	j.Runtime = ApplyRuntimeProfile(j.Runtime)
	if err := j.ValidateRuntimeLimits(); err != nil {
		return Job{}, RuntimeReport{}, err
	}
	report := RuntimeReport{}
	network := NetworkEnabled(j.Runtime)
	cacheDir := strings.TrimSpace(j.Runtime.Cache)

	for ref, input := range j.Inputs {
		if err := EnforceLocalPathPolicy(input, j.Runtime); err != nil {
			return Job{}, RuntimeReport{}, fmt.Errorf("input %q: %w", ref, err)
		}
		if !input.RequiresNetwork() {
			if err := EnforceAtomLimit(input, j.Runtime); err != nil {
				return Job{}, RuntimeReport{}, fmt.Errorf("input %q: %w", ref, err)
			}
			continue
		}
		resolved, err := input.ResolvedURL()
		if err != nil {
			return Job{}, RuntimeReport{}, err
		}
		if err := EnforceURLPolicy(resolved, j.Runtime); err != nil {
			return Job{}, RuntimeReport{}, fmt.Errorf("input %q: %w", ref, err)
		}
		if cacheDir == "" {
			if !network {
				return Job{}, RuntimeReport{}, fmt.Errorf("runtime network is disabled and input %q requires remote fetch without a cache", ref)
			}
			continue
		}
		cachePath := cachePathFor(cacheDir, resolved, input.ResolvedFormat())
		cached := PathExists(cachePath)
		if !cached {
			if !network {
				return Job{}, RuntimeReport{}, fmt.Errorf("runtime offline/network=false and cache miss for input %q (%s)", ref, resolved)
			}
			if err := downloadToCache(ctx, resolved, cachePath, maxDownloadBytes(j.Runtime), j.Runtime); err != nil {
				return Job{}, RuntimeReport{}, fmt.Errorf("cache input %q: %w", ref, err)
			}
			cached = false
		}
		entry, _ := cacheEntry(cachePath)
		format := input.Format
		if format == "" {
			format = input.ResolvedFormat()
		}
		input.ID = ""
		input.Provider = ""
		input.URL = ""
		input.Path = cachePath
		input.Format = format
		j.Inputs[ref] = input
		if err := EnforceAtomLimit(input, j.Runtime); err != nil {
			return Job{}, RuntimeReport{}, fmt.Errorf("input %q: %w", ref, err)
		}
		report.CachedInputs = append(report.CachedInputs, CachedInput{
			Ref:    ref,
			URL:    resolved,
			Path:   cachePath,
			Cached: cached,
			Bytes:  entry.Bytes,
		})
	}
	for i, output := range j.Outputs {
		if err := EnforceOutputPathPolicy(output, j.Runtime); err != nil {
			return Job{}, RuntimeReport{}, fmt.Errorf("outputs[%d]: %w", i, err)
		}
	}
	return j, report, nil
}

func EnforceAtomLimit(input Input, runtime Runtime) error {
	if runtime.MaxAtoms <= 0 {
		return nil
	}
	path := input.LocalPath()
	if strings.TrimSpace(path) == "" {
		return nil
	}
	count, counted, err := CountAtomsFile(path, input.ResolvedFormat())
	if err != nil {
		return err
	}
	if !counted {
		return nil
	}
	if count > runtime.MaxAtoms {
		return fmt.Errorf("atom count %d exceeds runtime.max_atoms=%d", count, runtime.MaxAtoms)
	}
	return nil
}

func CountAtomsFile(path string, format string) (int, bool, error) {
	normalized := NormalizeFormat(format)
	switch normalized {
	case "bcif", "xtc", "dcd", "trr", "nctraj":
		return 0, false, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "ATOM ") || strings.HasPrefix(line, "HETATM ") {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, false, err
	}
	return count, true, nil
}

func ExplainRuntime(j Job) []map[string]any {
	var actions []map[string]any
	cacheDir := strings.TrimSpace(j.Runtime.Cache)
	network := NetworkEnabled(j.Runtime)
	for ref, input := range j.Inputs {
		action := map[string]any{
			"ref":              ref,
			"requires_network": input.RequiresNetwork(),
			"network_enabled":  network,
			"format":           input.ResolvedFormat(),
		}
		if resolved, err := input.ResolvedURL(); err == nil {
			action["url"] = resolved
			if cacheDir != "" && input.RequiresNetwork() {
				path := cachePathFor(cacheDir, resolved, input.ResolvedFormat())
				action["cache_path"] = path
				action["cache_hit"] = PathExists(path)
			}
			if err := EnforceURLPolicy(resolved, j.Runtime); err != nil {
				action["blocked"] = err.Error()
			}
		}
		if err := EnforceLocalPathPolicy(input, j.Runtime); err != nil {
			action["blocked"] = err.Error()
		}
		actions = append(actions, action)
	}
	return actions
}

func CachePathFor(cacheDir, rawURL, format string) string {
	sum := sha256.Sum256([]byte(rawURL))
	ext := extensionForFormat(format)
	return filepath.Join(cacheDir, "downloads", hex.EncodeToString(sum[:])+ext)
}

func cachePathFor(cacheDir, rawURL, format string) string {
	return CachePathFor(cacheDir, rawURL, format)
}

func extensionForFormat(format string) string {
	switch NormalizeFormat(format) {
	case "bcif":
		return ".bcif"
	case "pdb":
		return ".pdb"
	default:
		return ".cif"
	}
}

func maxDownloadBytes(runtime Runtime) int64 {
	if runtime.MaxDownloadBytes > 0 {
		return runtime.MaxDownloadBytes
	}
	return defaultMaxDownloadBytes
}

func downloadToCache(ctx context.Context, rawURL, path string, limit int64, runtime Runtime) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("cache download only supports http(s), got %s", parsed.Scheme)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			// Re-apply the host allowlist on every hop so an allowed host
			// cannot redirect the fetch to an internal address (SSRF).
			return EnforceURLPolicy(req.URL.String(), runtime)
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("GET %s returned %s", rawURL, response.Status)
	}
	if response.ContentLength > limit {
		return fmt.Errorf("download is %d bytes, exceeding runtime.max_download_bytes=%d", response.ContentLength, limit)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, limit+1))
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if written > limit {
		_ = os.Remove(tmp)
		return fmt.Errorf("download exceeded runtime.max_download_bytes=%d", limit)
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return writeCacheEntry(rawURL, path)
}

func writeCacheEntry(rawURL, path string) error {
	entry, err := cacheEntry(path)
	if err != nil {
		return err
	}
	entry.URL = rawURL
	entry.Path = path
	entry.Format = strings.TrimPrefix(filepath.Ext(path), ".")
	entry.CachedAt = time.Now().UTC()
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path+".json", append(data, '\n'), 0o644)
}

func cacheEntry(path string) (CacheEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return CacheEntry{}, err
	}
	defer file.Close()
	hash := sha256.New()
	bytes, err := io.Copy(hash, file)
	if err != nil {
		return CacheEntry{}, err
	}
	return CacheEntry{
		Path:     path,
		Format:   strings.TrimPrefix(filepath.Ext(path), "."),
		Bytes:    bytes,
		SHA256:   hex.EncodeToString(hash.Sum(nil)),
		Verified: true,
	}, nil
}

func ReadCacheEntry(path string) (CacheEntry, error) {
	data, err := os.ReadFile(path + ".json")
	if err == nil {
		var entry CacheEntry
		if err := json.Unmarshal(data, &entry); err == nil {
			current, currentErr := cacheEntry(path)
			if currentErr == nil {
				entry.Bytes = current.Bytes
				entry.Verified = current.SHA256 == entry.SHA256
			}
			return entry, nil
		}
	}
	return cacheEntry(path)
}

func ListCache(cacheDir string) ([]CacheEntry, error) {
	downloads := filepath.Join(cacheDir, "downloads")
	var entries []CacheEntry
	err := filepath.WalkDir(downloads, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Cache files are hex-named (sha256), so any dot-prefixed entry is not a
		// real cache file: skip directories, sidecars, temp files, and hidden
		// entries such as macOS AppleDouble "._*" files created when the cache
		// lives on a non-native filesystem (exFAT, SMB, FAT).
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") || strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		entry, err := ReadCacheEntry(path)
		if err != nil {
			return err
		}
		entries = append(entries, entry)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return entries, err
}

func PruneCache(cacheDir string, olderThan time.Duration, dryRun bool) ([]string, error) {
	entries, err := ListCache(cacheDir)
	if err != nil {
		return nil, err
	}
	var removed []string
	cutoff := time.Now().Add(-olderThan)
	for _, entry := range entries {
		info, err := os.Stat(entry.Path)
		if err != nil {
			continue
		}
		if olderThan > 0 && info.ModTime().After(cutoff) {
			continue
		}
		removed = append(removed, entry.Path)
		if !dryRun {
			_ = os.Remove(entry.Path)
			_ = os.Remove(entry.Path + ".json")
		}
	}
	return removed, nil
}

func EnforceURLPolicy(rawURL string, runtime Runtime) error {
	if len(runtime.AllowHosts) == 0 {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range runtime.AllowHosts {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == host || strings.HasPrefix(allowed, "*.") && strings.HasSuffix(host, strings.TrimPrefix(allowed, "*")) {
			return nil
		}
	}
	return fmt.Errorf("host %q is not allowed", host)
}

func EnforceLocalPathPolicy(input Input, runtime Runtime) error {
	return enforcePathWithinRoots(input.LocalPath(), runtime)
}

// EnforceOutputPathPolicy confines a job output path to the configured allowed
// roots. Without this, a server whose operator set --allow-path to sandbox
// input reads would still let a client write (and read back) rendered files
// anywhere the process can reach.
func EnforceOutputPathPolicy(output Output, runtime Runtime) error {
	return enforcePathWithinRoots(output.Path, runtime)
}

func enforcePathWithinRoots(path string, runtime Runtime) error {
	if len(runtime.AllowPaths) == 0 {
		return nil
	}
	if strings.TrimSpace(path) == "" {
		return nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// Resolve symlinks so a link planted inside an allowed root cannot redirect
	// the effective target outside it. filepath.Abs already cleans "..", but a
	// symlink is only followed by EvalSymlinks. Roots are resolved the same way
	// so the comparison is apples-to-apples (e.g. /var -> /private/var on macOS).
	resolved := resolveSymlinkPrefix(absolute)
	for _, allowed := range runtime.AllowPaths {
		if strings.TrimSpace(allowed) == "" {
			continue
		}
		root, err := filepath.Abs(allowed)
		if err != nil {
			return err
		}
		root = resolveSymlinkPrefix(root)
		if resolved == root || strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
			return nil
		}
	}
	roots := slices.Clone(runtime.AllowPaths)
	return fmt.Errorf("path %q is outside allowed roots %v", absolute, roots)
}

// resolveSymlinkPrefix resolves symlinks in the longest existing ancestor of
// path and re-appends the non-existent remainder. This follows symlinks that
// exist (closing the bypass where a link inside a root points outside it) while
// still supporting output paths that have not been created yet.
func resolveSymlinkPrefix(path string) string {
	remainder := ""
	current := path
	for {
		if evaluated, err := filepath.EvalSymlinks(current); err == nil {
			if remainder == "" {
				return evaluated
			}
			return filepath.Join(evaluated, remainder)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path // nothing along the path exists; use the cleaned literal
		}
		if remainder == "" {
			remainder = filepath.Base(current)
		} else {
			remainder = filepath.Join(filepath.Base(current), remainder)
		}
		current = parent
	}
}
