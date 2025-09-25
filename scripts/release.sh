#!/bin/bash

# PGMCP Release Script
# Builds binaries for multiple platforms and creates release archives

set -e

VERSION=${1:-"dev"}
COMMIT=$(git rev-parse HEAD || echo "unknown")
DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

if [ "$VERSION" = "dev" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v1.0.0"
    exit 1
fi

echo "Building PGMCP $VERSION"
echo "Commit: $COMMIT"
echo "Date: $DATE"

# Create build directory
BUILD_DIR="build"
RELEASE_DIR="$BUILD_DIR/release"
rm -rf "$BUILD_DIR"
mkdir -p "$RELEASE_DIR"

# Build flags
LDFLAGS="-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE"

# Platforms to build for
declare -a PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
    "windows/amd64"
    "windows/arm64"
)

echo "Building binaries..."

for platform in "${PLATFORMS[@]}"; do
    IFS='/' read -r goos goarch <<< "$platform"
    
    echo "  Building for $goos/$goarch..."
    
    # Create platform directory
    platform_dir="$BUILD_DIR/$goos-$goarch"
    mkdir -p "$platform_dir"
    
    # Set binary names (add .exe for Windows)
    server_binary="pgmcp-server"
    client_binary="pgmcp-client"
    if [ "$goos" = "windows" ]; then
        server_binary="pgmcp-server.exe"
        client_binary="pgmcp-client.exe"
    fi
    
    # Build server
    env GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags "$LDFLAGS" \
        -o "$platform_dir/$server_binary" \
        ./server
    
    # Build client
    env GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build \
        -trimpath \
        -ldflags "$LDFLAGS" \
        -o "$platform_dir/$client_binary" \
        ./client
    
    # Copy additional files
    cp README.md "$platform_dir/"
    cp LICENSE "$platform_dir/"
    cp schema.sql "$platform_dir/"
    cp schema_minimal.sql "$platform_dir/"
    
    # Create archive
    archive_name="pgmcp_${VERSION}_${goos}_${goarch}"
    if [ "$goos" = "windows" ]; then
        cd "$BUILD_DIR" && zip -r "$RELEASE_DIR/$archive_name.zip" "${goos}-${goarch}/" && cd ..
    else
        cd "$BUILD_DIR" && tar -czf "$RELEASE_DIR/$archive_name.tar.gz" "${goos}-${goarch}/" && cd ..
    fi
    
    echo "    Created $archive_name"
done

# Generate checksums
echo "Generating checksums..."
cd "$RELEASE_DIR"
if command -v sha256sum &> /dev/null; then
    sha256sum *.tar.gz *.zip > checksums.txt
elif command -v shasum &> /dev/null; then
    shasum -a 256 *.tar.gz *.zip > checksums.txt
else
    echo "Warning: No checksum utility found (sha256sum or shasum)"
fi
cd - > /dev/null

echo ""
echo "Release build complete!"
echo "Archives created in: $RELEASE_DIR"
echo ""
echo "To create a GitHub release:"
echo "1. Create and push a git tag: git tag $VERSION && git push origin $VERSION"
echo "2. The GitHub Actions workflow will automatically create the release"
echo ""
echo "Or upload manually:"
echo "gh release create $VERSION $RELEASE_DIR/* --title \"PGMCP $VERSION\" --notes \"Release $VERSION\""
