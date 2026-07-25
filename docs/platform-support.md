# Platform Support

Headless Mol* has two different support levels:

1. **Contract support**: the CLI builds, validates jobs, prints schemas/docs/completions, runs dry-run rendering, and exposes stable JSON contracts.
2. **Renderer support**: the packaged Mol* renderer can create real images or videos with headless WebGL.

## Support Matrix

| Platform | CLI contract | Packaged artifact | Renderer smoke | Notes |
| --- | --- | --- | --- | --- |
| Linux amd64 | Required | Required | Required | Primary release platform and Docker image target. |
| Linux arm64 | Expected | Expected | Best effort | CLI and archive are built; renderer depends on native GL/canvas availability. |
| macOS arm64/amd64 | Required | Required | Best effort | Local rendering works when native Node canvas/GL dependencies install cleanly. |
| Windows amd64/arm64 | Required | Required | Best effort | Contract smoke is expected; renderer support depends on native dependency availability. |

## Release Gates

`npm run test:release` is the local release gate. It runs Go tests, `go vet`, TypeScript typechecking, renderer JavaScript syntax checking, generated asset drift checks, local packaging, and installed-artifact smoke tests.

`npm run release:candidate` wraps the release gate, copies the produced artifact into a release-candidate folder, writes a SHA-256 file, and emits `manifest.json`.

`npm run release:candidate -- --reuse-verified-artifact` skips the gate/package work and only wraps an already verified local artifact in a release-candidate manifest/copy.

On Linux CI, run:

```bash
npm run release:candidate -- --docker
```

That adds Docker runtime smoke and Docker packaged-artifact smoke. For the strict visual renderer gate, run:

```bash
npm run release:candidate -- --docker --render-docker
```

The Docker artifact smoke intentionally requires a Linux artifact. On macOS or Windows, run `npm run release:candidate` without `--docker`; use the uploaded Linux release-candidate artifact for Docker verification.

## Artifact Manifest

Each release candidate writes:

```text
dist/release-candidates/<label>/manifest.json
dist/release-candidates/<label>/headlessmolstar-<label>-<goos>-<goarch>.<ext>
dist/release-candidates/<label>/headlessmolstar-<label>-<goos>-<goarch>.<ext>.sha256
dist/release-candidates/<label>/release-candidate.log
```

The manifest records the git commit, dirty-worktree flag, platform, copied artifact path, artifact byte size, SHA-256, and the status of release, artifact verification, Docker runtime, Docker artifact, and Docker render checks.
