# Release Process

This document outlines the release process for `azrbnkctl`.

## 1. Automated Release Flow

`azrbnkctl` uses **Google Release Please** and **GoReleaser** for automated semantic versioning and release publishing:

1. Merge approved PRs into the `staging` branch.
2. Promote `staging` to `main` via PR.
3. On push to `main`, the `release-please` GitHub Actions workflow analyzes Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`) and generates/updates a Release PR.
4. When the Release PR is merged, Release Please creates a git tag (`vX.Y.Z`).
5. The `goreleaser` workflow triggers on the tag, builds multi-platform binaries (Linux, macOS, Windows on amd64 and arm64), and attaches them to the GitHub Release with checksums.

## 2. Local Release Verification

Before tagging a release, run a local snapshot build:

```bash
make goreleaser-check
make goreleaser-snapshot
```
