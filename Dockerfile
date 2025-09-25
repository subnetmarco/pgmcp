FROM scratch

# Copy the binaries from the build context (GoReleaser will place them here)
COPY pgmcp-server /pgmcp-server
COPY pgmcp-client /pgmcp-client

# Expose the default port
EXPOSE 8080

# Set the binary as the entrypoint
ENTRYPOINT ["/pgmcp-server"]
