package job

import (
	"fmt"
	"os"
	"strings"
)

const (
	RuntimeProfileDefault = ""
	RuntimeProfileCI      = "ci"
	RuntimeProfileLocked  = "locked"
)

func ApplyRuntimeProfile(runtime Runtime) Runtime {
	switch strings.ToLower(strings.TrimSpace(runtime.Profile)) {
	case RuntimeProfileDefault, "default":
		if runtime.Profile == "default" {
			runtime.Profile = ""
		}
		return runtime
	case RuntimeProfileCI:
		if runtime.Cache == "" {
			runtime.Cache = ".molstar-cache"
		}
		if runtime.TimeoutSeconds == 0 {
			runtime.TimeoutSeconds = 120
		}
		if runtime.MaxPixels == 0 {
			runtime.MaxPixels = 16_000_000
		}
		if runtime.MaxAtoms == 0 {
			runtime.MaxAtoms = 250_000
		}
		if runtime.MaxOutputs == 0 {
			runtime.MaxOutputs = 16
		}
		if runtime.MaxDownloadBytes == 0 {
			runtime.MaxDownloadBytes = defaultMaxDownloadBytes
		}
		if runtime.MaxArchiveBytes == 0 {
			runtime.MaxArchiveBytes = defaultMaxArchiveBytes
		}
		return runtime
	case RuntimeProfileLocked:
		network := false
		runtime.Network = &network
		runtime.Offline = true
		if runtime.TimeoutSeconds == 0 {
			runtime.TimeoutSeconds = 60
		}
		if runtime.MaxPixels == 0 {
			runtime.MaxPixels = 4_000_000
		}
		if runtime.MaxAtoms == 0 {
			runtime.MaxAtoms = 100_000
		}
		if runtime.MaxOutputs == 0 {
			runtime.MaxOutputs = 4
		}
		if runtime.MaxDownloadBytes == 0 {
			runtime.MaxDownloadBytes = 64 << 20
		}
		if runtime.MaxArchiveBytes == 0 {
			runtime.MaxArchiveBytes = 64 << 20
		}
		if len(runtime.AllowPaths) == 0 {
			if cwd, err := os.Getwd(); err == nil {
				runtime.AllowPaths = []string{cwd}
			}
		}
		return runtime
	default:
		return runtime
	}
}

func ValidateRuntimeProfile(runtime Runtime) error {
	switch strings.ToLower(strings.TrimSpace(runtime.Profile)) {
	case "", "default", RuntimeProfileCI, RuntimeProfileLocked:
		return nil
	default:
		return fmt.Errorf("unsupported runtime.profile %q", runtime.Profile)
	}
}
