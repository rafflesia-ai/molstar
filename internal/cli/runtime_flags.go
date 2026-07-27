package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
)

type runtimeFlags struct {
	profile          string
	cache            string
	noCache          bool
	offline          bool
	noNetwork        bool
	timeoutSeconds   int
	maxPixels        int
	maxAtoms         int
	maxOutputs       int
	maxDownloadBytes int64
	allowHosts       []string
	allowPaths       []string
}

func bindRuntimeFlags(cmd *cobra.Command, flags *runtimeFlags) {
	cmd.Flags().StringVar(&flags.profile, "profile", "", "runtime profile: default, ci, or locked")
	cmd.Flags().StringVar(&flags.cache, "cache", "", "download cache directory for remote inputs")
	cmd.Flags().StringVar(&flags.cache, "cache-dir", "", "alias for --cache")
	cmd.Flags().BoolVar(&flags.noCache, "no-cache", false, "disable the download cache even if the job file configures one")
	cmd.Flags().BoolVar(&flags.offline, "offline", false, "disable network and require cached/local inputs")
	cmd.Flags().BoolVar(&flags.noNetwork, "no-network", false, "disable network access")
	cmd.Flags().IntVar(&flags.timeoutSeconds, "timeout", 0, "job timeout in seconds")
	cmd.Flags().IntVar(&flags.maxPixels, "max-pixels", 0, "maximum pixels per image/video output")
	cmd.Flags().IntVar(&flags.maxAtoms, "max-atoms", 0, "maximum atoms per local/cached structure when countable")
	cmd.Flags().IntVar(&flags.maxOutputs, "max-outputs", 0, "maximum number of outputs")
	cmd.Flags().Int64Var(&flags.maxDownloadBytes, "max-download-bytes", 0, "maximum bytes per cached remote download")
	cmd.Flags().StringArrayVar(&flags.allowHosts, "allow-host", nil, "allow remote input host; repeat for multiple hosts")
	cmd.Flags().StringArrayVar(&flags.allowPaths, "allow-path", nil, "allow local input root path; repeat for multiple roots")
}

func applyRuntimeFlags(cmd *cobra.Command, j *job.Job, flags runtimeFlags) {
	j.Runtime = runtimeFromFlags(cmd, j.Runtime, flags)
}

func runtimeFromFlags(cmd *cobra.Command, base job.Runtime, flags runtimeFlags) job.Runtime {
	if cmd.Flags().Changed("profile") {
		base.Profile = flags.profile
		base = job.ApplyRuntimeProfile(base)
	}
	if cmd.Flags().Changed("cache") {
		base.Cache = flags.cache
	}
	if cmd.Flags().Changed("cache-dir") {
		base.Cache = flags.cache
	}
	if cmd.Flags().Changed("no-cache") && flags.noCache {
		base.Cache = ""
	}
	if cmd.Flags().Changed("offline") {
		base.Offline = flags.offline
		if flags.offline {
			network := false
			base.Network = &network
		}
	}
	if cmd.Flags().Changed("no-network") {
		network := !flags.noNetwork
		base.Network = &network
	}
	if cmd.Flags().Changed("timeout") {
		base.TimeoutSeconds = flags.timeoutSeconds
	}
	if cmd.Flags().Changed("max-pixels") {
		base.MaxPixels = flags.maxPixels
	}
	if cmd.Flags().Changed("max-atoms") {
		base.MaxAtoms = flags.maxAtoms
	}
	if cmd.Flags().Changed("max-outputs") {
		base.MaxOutputs = flags.maxOutputs
	}
	if cmd.Flags().Changed("max-download-bytes") {
		base.MaxDownloadBytes = flags.maxDownloadBytes
	}
	if cmd.Flags().Changed("allow-host") {
		base.AllowHosts = flags.allowHosts
	}
	if cmd.Flags().Changed("allow-path") {
		base.AllowPaths = flags.allowPaths
	}
	return job.ApplyRuntimeProfile(base)
}

func contextWithRuntimeTimeout(ctx context.Context, runtime job.Runtime) (context.Context, context.CancelFunc) {
	runtime = job.ApplyRuntimeProfile(runtime)
	if runtime.TimeoutSeconds <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(runtime.TimeoutSeconds)*time.Second)
}
