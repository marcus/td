# Releasing a New Version

Guide for creating new td releases.

## How It Works

Releases are automated via [GoReleaser](https://goreleaser.com/) and GitHub Actions. When you push a version tag (`v*`), the release workflow:

1. Builds binaries for darwin/linux × amd64/arm64
2. Creates a GitHub release with binary assets and checksums
3. Generates a changelog from commits since the last tag
4. Pushes a Homebrew cask (`Casks/td.rb`) to `marcus/homebrew-tap`

## Prerequisites

- Clean working tree (`git status` shows no changes)
- All tests passing (`go test ./...`)
- On the main branch
- GitHub CLI authenticated (`gh auth status`)
- `HOMEBREW_TAP_TOKEN` secret configured on the repo (for Homebrew cask push)

## Release Process

### 1. Determine Version

Follow semantic versioning:
- **Major** (v1.0.0): Breaking changes
- **Minor** (v0.5.0): New features, backward compatible
- **Patch** (v0.4.26): Bug fixes only

Check current version:
```bash
git tag -l | sort -V | tail -1
```

### 2. Update CHANGELOG.md

Add entry at the top of `CHANGELOG.md`:

```markdown
## [vX.Y.Z] - YYYY-MM-DD

### Features
- New feature description

### Bug Fixes
- Fix description

### Documentation
- Doc change description
```

Commit the changelog:
```bash
git add CHANGELOG.md
git commit -m "docs: Update changelog for vX.Y.Z"
```

### 3. Verify Tests Pass

```bash
go test ./...
```

### 4. Create Tag and Push

```bash
# Push any pending commits
git push origin main

# Create annotated tag and push it (triggers automated release)
git tag -a vX.Y.Z -m "Release vX.Y.Z: brief description"
git push origin vX.Y.Z
```

Pushing the tag triggers `.github/workflows/release.yml`, which runs GoReleaser to build binaries, create the GitHub release, and update the Homebrew cask.

### 5. Verify

```bash
# Watch the release workflow
gh run watch

# Check release exists with binary assets
gh release view vX.Y.Z

# Install via Homebrew (or upgrade)
brew install marcus/tap/td
# or: brew upgrade td

# Verify version
td version
```

## Local Development Builds

For local installs without a release:

```bash
# Install with git-described version
make install-dev

# Or manually specify version
go install -ldflags "-X main.Version=vX.Y.Z" ./...
```

## Local Snapshot (Test GoReleaser Without Publishing)

```bash
goreleaser release --snapshot --clean
ls dist/  # inspect built binaries and archives
```

## Version in Binaries

Version is embedded at build time via ldflags (`-X main.Version=...`). GoReleaser handles this automatically for releases. Without ldflags, version defaults to `devel` or shows git info.

## Update Mechanism

Users see update notifications because:
1. On `td version`, it checks `https://api.github.com/repos/marcus/td/releases/latest`
2. Compares `tag_name` against current version
3. Shows update command if newer version exists
4. Results cached for 6 hours in `~/.cache/td/version-cache.json`

Dev versions (`devel`, `devel+hash`) skip the check.

## Quick Release (Copy-Paste)

Replace `X.Y.Z` with actual version:

```bash
# Verify clean state
git status
go test ./...

# Update changelog
# (Edit CHANGELOG.md, add entry at top)
git add CHANGELOG.md
git commit -m "docs: Update changelog for vX.Y.Z"

# Push commits, then tag (tag push triggers automated release)
git push origin main
git tag -a vX.Y.Z -m "Release vX.Y.Z: brief description"
git push origin vX.Y.Z

# Verify (wait for workflow to complete)
gh run watch
gh release view vX.Y.Z
brew upgrade td && td version
```

## Checklist

- [ ] Tests pass (`go test ./...`)
- [ ] Working tree clean
- [ ] CHANGELOG.md updated with new version entry
- [ ] Changelog committed to git
- [ ] Version number follows semver
- [ ] Commits pushed to main
- [ ] Tag created with `-a` (annotated)
- [ ] Tag pushed to origin (triggers GoReleaser)
- [ ] GitHub release has binary assets (automated)
- [ ] Homebrew cask updated in `marcus/homebrew-tap` (automated)
- [ ] `brew install marcus/tap/td` works
- [ ] `td version` shows correct version
