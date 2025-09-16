// client/main.go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type asksFlag []string
func (a *asksFlag) String() string { return strings.Join(*a, "; ") }
func (a *asksFlag) Set(v string) error { *a = append(*a, v); return nil }

func main() {
	url := getenv("PGMCP_SERVER_URL", "http://127.0.0.1:8080/mcp/sse")
	bearer := os.Getenv("PGMCP_AUTH_BEARER")

	serverURL := flag.String("url", url, "MCP SSE server URL (e.g. http://host:8080/mcp/sse)")
	auth := flag.String("bearer", bearer, "Optional bearer token")
	var asks asksFlag
	flag.Var(&asks, "ask", "Plain-English question to run (repeatable)")
	search := flag.String("search", "", "Optional free-text search string")
	flag.Parse()

	httpClient := &http.Client{
		Transport: &authRoundTripper{base: http.DefaultTransport, bearer: strings.TrimSpace(*auth)},
	}
	tr := &mcp.SSEClientTransport{
		Endpoint:   *serverURL,
		HTTPClient: httpClient,
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "pgmcp-client", Version: "0.3.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, tr, nil)
	if err != nil { log.Fatalf("connect to %s failed: %v", *serverURL, err) }
	defer session.Close()

	if _, err := session.ListTools(ctx, &mcp.ListToolsParams{}); err != nil {
		log.Fatalf("tools/list failed: %v", err)
	}

	for _, q := range asks {
		runAsk(ctx, session, q)
	}
	if s := strings.TrimSpace(*search); s != "" {
		runSearch(ctx, session, s)
	}
}

func runAsk(ctx context.Context, session *mcp.ClientSession, question string) {
	args := map[string]any{"query": question, "max_rows": 20}
	call(ctx, session, "ask", args)
}
func runSearch(ctx context.Context, session *mcp.ClientSession, q string) {
	args := map[string]any{"q": q, "limit": 20}
	call(ctx, session, "search", args)
}

func call(ctx context.Context, session *mcp.ClientSession, tool string, args map[string]any) {
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil { log.Fatalf("%s failed: %v", tool, err) }
	if res.IsError { printContent(res.Content); log.Fatalf("%s returned error", tool) }

	if res.StructuredContent != nil {
		b, _ := json.MarshalIndent(res.StructuredContent, "", "  ")
		fmt.Println(string(b))
		return
	}
	printContent(res.Content)
}

func printContent(cs []mcp.Content) {
	for _, c := range cs {
		switch v := c.(type) {
		case *mcp.TextContent:
			if pretty, ok := tryPrettyJSON(v.Text); ok { fmt.Println(pretty) } else { fmt.Println(v.Text) }
		case *mcp.ImageContent:
			out := map[string]any{"type": "image", "mimeType": v.MIMEType}
			b, _ := json.MarshalIndent(out, "", "  "); fmt.Println(string(b))
		default:
			b, _ := json.MarshalIndent(v, "", "  "); fmt.Println(string(b))
		}
	}
}

type authRoundTripper struct {
	base   http.RoundTripper
	bearer string
}
func (rt *authRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	if rt.bearer != "" { r.Header.Set("Authorization", "Bearer "+rt.bearer) }
	return rt.base.RoundTrip(r)
}

func tryPrettyJSON(s string) (string, bool) {
	var anyJSON any
	if err := json.Unmarshal([]byte(s), &anyJSON); err != nil { return "", false }
	b, _ := json.MarshalIndent(anyJSON, "", "  ")
	return string(b), true
}
func getenv(k, def string) string { if v := os.Getenv(k); v != "" { return v }; return def }
