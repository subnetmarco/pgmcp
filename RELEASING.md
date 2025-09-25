# Release Process

This document describes how to create releases for PGMCP.

## Automated Releases (GitHub Actions)

The project uses GitHub Actions and GoReleaser for automated releases.

### Creating a Release

1. **Ensure main branch is ready**:
   ```bash
   git checkout main
   git pull origin main
   ```

2. **Create and push a git tag**:
   ```bash
   # Create a new tag (use semantic versioning)
   git tag v1.0.0
   
   # Push the tag to trigger the release workflow
   git push origin v1.0.0
   ```

3. **Monitor the workflow**:
   - Go to [GitHub Actions](https://github.com/subnetmarco/pgmcp/actions)
   - Watch the "release" workflow complete
   - The workflow will:
     - Build binaries for all platforms
     - Create archives (tar.gz for Unix, zip for Windows)
     - Generate checksums
     - Create a GitHub release with all assets
     - Build and push Docker images
     - Update Homebrew formula (if configured)

### What Gets Built

The release workflow builds:

- **Platforms**: Linux, macOS, Windows
- **Architectures**: amd64, arm64
- **Binaries**: `pgmcp-server` and `pgmcp-client`
- **Archives**: Platform-specific archives with binaries + documentation
- **Docker**: Multi-platform Docker images
- **Packages**: Debian/RPM packages (future)
- **Homebrew**: Formula updates (future)

### Assets Included in Each Release

- `pgmcp-server` and `pgmcp-client` binaries
- `README.md` - Project documentation
- `LICENSE` - License file
- `schema.sql` - Full database schema
- `schema_minimal.sql` - Minimal test schema
- `checksums.txt` - SHA256 checksums for verification

## Manual Releases

For testing or when you need more control:

### Local Build Script

```bash
# Build all platforms locally
./scripts/release.sh v1.0.0

# Outputs to build/release/
ls build/release/
```

### Test Build

```bash
# Test that all platforms compile
./scripts/test-build.sh
```

### Manual Upload

```bash
# After running the release script
gh release create v1.0.0 build/release/* \
  --title "PGMCP v1.0.0" \
  --notes "Release notes here"
```

## Release Checklist

Before creating a release:

- [ ] All tests pass (`go test ./...`)
- [ ] Integration tests pass (`go test ./server -tags=integration`)
- [ ] No linting errors (`go vet ./...`, `gofmt -s -l .`)
- [ ] Documentation is up to date
- [ ] Version number follows [semantic versioning](https://semver.org/)
- [ ] Test build script passes (`./scripts/test-build.sh`)

## Version Schema

Use [semantic versioning](https://semver.org/):

- `vX.Y.Z` for stable releases
- `vX.Y.Z-rc.N` for release candidates
- `vX.Y.Z-beta.N` for beta releases
- `vX.Y.Z-alpha.N` for alpha releases

Examples:
- `v1.0.0` - Major release
- `v1.1.0` - Minor release (new features, backwards compatible)
- `v1.1.1` - Patch release (bug fixes)
- `v2.0.0-rc.1` - Release candidate

## Troubleshooting

### Release Workflow Fails

1. Check [GitHub Actions logs](https://github.com/subnetmarco/pgmcp/actions)
2. Common issues:
   - Build failures (check Go version compatibility)
   - GoReleaser configuration errors
   - Missing permissions for creating releases

### Fix a Failed Release

```bash
# Delete the tag locally and remotely
git tag -d v1.0.0
git push origin :refs/tags/v1.0.0

# Delete the draft release on GitHub
gh release delete v1.0.0

# Fix issues, then recreate the tag
git tag v1.0.0
git push origin v1.0.0
```

### Test GoReleaser Locally

```bash
# Install GoReleaser
go install github.com/goreleaser/goreleaser@latest

# Test the configuration (dry run)
goreleaser check

# Test build without releasing
goreleaser build --clean --snapshot
```

## Docker Images

Docker images are automatically built and pushed to:
- `ghcr.io/subnetmarco/pgmcp:latest`
- `ghcr.io/subnetmarco/pgmcp:v1.0.0`

### Manual Docker Build

```bash
# Build locally
docker build -t pgmcp:local .

# Test
docker run -e DATABASE_URL="..." -e OPENAI_API_KEY="..." -p 8080:8080 pgmcp:local
```
