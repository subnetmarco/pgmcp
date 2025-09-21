package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestStreamableHTTPTransport(t *testing.T) {
	db := mustPool(t)
	defer db.Close()

	// Use the standard resetSchema which creates tables that work
	resetSchema(t, db)

	// Setup server
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		QueryTO:     10 * time.Second,
		MaxRows:     50,
		SchemaTTL:   1 * time.Minute,
	}
	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	// Create MCP server with streamable HTTP handler
	impl := &mcp.Implementation{Name: "pgmcp-test", Version: "0.1.0"}
	server := mcp.NewServer(impl, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Search test",
	}, srv.handleSearch)

	// Create HTTP test server with streamable handler
	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)
	testServer := httptest.NewServer(handler)
	defer testServer.Close()

	t.Run("streamable_http_headers", func(t *testing.T) {
		// Test that the streamable HTTP transport returns correct headers
		req, _ := http.NewRequest("POST", testServer.URL, strings.NewReader(`{"jsonrpc": "2.0", "method": "tools/list", "id": 1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		defer resp.Body.Close()

		// Check required headers for streamable transport
		if resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("Expected Content-Type: text/event-stream, got: %s", resp.Header.Get("Content-Type"))
		}

		if resp.Header.Get("Cache-Control") != "no-cache, no-transform" {
			t.Fatalf("Expected Cache-Control: no-cache, no-transform, got: %s", resp.Header.Get("Cache-Control"))
		}

		t.Logf("✅ All required HTTP headers present for streamable transport")
		t.Logf("Content-Type: %s", resp.Header.Get("Content-Type"))
		t.Logf("Cache-Control: %s", resp.Header.Get("Cache-Control"))
		t.Logf("Connection: %s", resp.Header.Get("Connection"))
	})

	t.Run("streamable_event_format", func(t *testing.T) {
		// Test that responses use proper event-stream format
		req, _ := http.NewRequest("POST", testServer.URL, strings.NewReader(`{"jsonrpc": "2.0", "method": "initialize", "params": {"protocolVersion": "2024-11-05", "capabilities": {}, "clientInfo": {"name": "test", "version": "1.0"}}, "id": 1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("HTTP request failed: %v", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("Failed to read response: %v", err)
		}

		bodyStr := string(body)

		// Check for event-stream format
		if !strings.Contains(bodyStr, "event:") {
			t.Fatalf("Expected event-stream format with 'event:' field, got: %s", bodyStr)
		}

		if !strings.Contains(bodyStr, "data:") {
			t.Fatalf("Expected event-stream format with 'data:' field, got: %s", bodyStr)
		}

		t.Logf("✅ Proper event-stream format")
		t.Logf("Response body: %s", bodyStr)
	})

	t.Run("mcp_protocol_compliance", func(t *testing.T) {
		// Test that we can use the MCP client properly with streamable transport
		httpClient := &http.Client{}
		tr := &mcp.StreamableClientTransport{
			Endpoint:   testServer.URL,
			HTTPClient: httpClient,
		}

		client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		session, err := client.Connect(ctx, tr, nil)
		if err != nil {
			t.Fatalf("Failed to connect via streamable transport: %v", err)
		}
		defer session.Close()

		// Test tools/list
		tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("Failed to list tools: %v", err)
		}

		// Verify we have the search tool
		found := false
		for _, tool := range tools.Tools {
			if tool.Name == "search" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Expected to find 'search' tool in tools list")
		}

		t.Logf("✅ MCP client successfully connected via streamable HTTP transport")
		t.Logf("✅ Found %d tools including 'search'", len(tools.Tools))
	})
}
