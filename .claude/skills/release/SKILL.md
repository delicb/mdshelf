---
name: release
description: Use when publishing a new mdshelf release - cutting a version, tagging, or shipping release binaries.
---

# Publish an MDShelf release

## Pre-flight

1. Confirm CI is green for the tip of `main` in the [CI workflow](https://github.com/delicb/mdshelf/actions/workflows/ci.yml).
2. Confirm the working tree is clean and `main` is pushed.
3. Pick the next semantic version `vMAJOR.MINOR.PATCH`.
4. Search the repo for the previous version string and check that any version references are consistent.

## Procedure

Push `main`, then create and push an annotated semver tag:

```sh
git push origin main
git tag -a vX.Y.Z -m "MDShelf vX.Y.Z"
git push origin vX.Y.Z
```

The release workflow (`.github/workflows/release.yml`) accepts `vMAJOR.MINOR.PATCH` tags only and verifies that the tagged commit belongs to `main`. It tests the code, scans known Go vulnerabilities, builds archives for macOS, Linux, and Windows on AMD64 and ARM64, generates a `SHA256SUMS` file, and creates the GitHub release.

## After tagging

- Watch the release workflow run until the GitHub release exists.
- If the run fails, fix the cause on `main` and tag the next patch version. Do not move or reuse a pushed tag.
