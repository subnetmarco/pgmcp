// client/main.go
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
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
	format := flag.String("format", "json", "Output format: table, json, csv")
	verbose := flag.Bool("verbose", false, "Verbose output")
	var asks asksFlag
	flag.Var(&asks, "ask", "Plain-English question to run (repeatable)")
	search := flag.String("search", "", "Optional free-text search string")
	flag.Parse()

	// Validate format
	validFormats := map[string]bool{"table": true, "json": true, "csv": true}
	if !validFormats[*format] {
		log.Fatalf("Invalid format '%s', must be one of: table, json, csv", *format)
	}

	httpClient := &http.Client{
		Transport: &authRoundTripper{base: http.DefaultTransport, bearer: strings.TrimSpace(*auth)},
	}
	tr := &mcp.SSEClientTransport{
		Endpoint:   *serverURL,
		HTTPClient: httpClient,
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "pgmcp-client", Version: "0.4.0"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, tr, nil)
	if err != nil { log.Fatalf("connect to %s failed: %v", *serverURL, err) }
	defer session.Close()

	if _, err := session.ListTools(ctx, &mcp.ListToolsParams{}); err != nil {
		log.Fatalf("tools/list failed: %v", err)
	}

	if *verbose {
		fmt.Printf("✓ Connected to server at %s\n", *serverURL)
	}

	for _, q := range asks {
		if *verbose {
			fmt.Printf("🔍 Asking: %s\n", q)
		}
		runAsk(ctx, session, q, *format, *verbose)
	}
	if s := strings.TrimSpace(*search); s != "" {
		if *verbose {
			fmt.Printf("🔎 Searching for: %s\n", s)
		}
		runSearch(ctx, session, s, *format, *verbose)
	}
}

func runAsk(ctx context.Context, session *mcp.ClientSession, question, format string, verbose bool) {
	args := map[string]any{"query": question, "max_rows": 50}
	call(ctx, session, "ask", args, format, verbose)
}
func runSearch(ctx context.Context, session *mcp.ClientSession, q, format string, verbose bool) {
	args := map[string]any{"q": q, "limit": 50}
	call(ctx, session, "search", args, format, verbose)
}

func call(ctx context.Context, session *mcp.ClientSession, tool string, args map[string]any, format string, verbose bool) {
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil { log.Fatalf("%s failed: %v", tool, err) }
	if res.IsError { printContent(res.Content); log.Fatalf("%s returned error", tool) }

	if res.StructuredContent != nil {
		// Convert to map and use enhanced formatting
		var result map[string]any
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, &result); err == nil {
			printFormattedResult(result, format, verbose)
			return
		}
		
		// Fallback to original
		b, _ = json.MarshalIndent(res.StructuredContent, "", "  ")
		fmt.Println(string(b))
		return
	}
	printContent(res.Content)
}

func printFormattedResult(result map[string]any, format string, verbose bool) {
	// Extract rows and SQL if present
	rows, hasRows := result["rows"].([]any)
	sql, hasSQL := result["sql"].(string)
	
	if verbose && hasSQL {
		fmt.Printf("📝 Generated SQL: %s\n\n", sql)
	}

	if !hasRows || len(rows) == 0 {
		if format == "json" {
			printJSON(result)
			return
		}
		fmt.Println("No results found.")
		return
	}

	switch format {
	case "table":
		printTable(rows)
	case "csv":
		printCSV(rows)
	case "json":
		printJSON(result)
	default:
		printJSON(result)
	}
}

func printTable(rows []any) {
	if len(rows) == 0 {
		return
	}

	// Convert to []map[string]any
	var records []map[string]any
	for _, row := range rows {
		if record, ok := row.(map[string]any); ok {
			records = append(records, record)
		}
	}

	if len(records) == 0 {
		return
	}

	// Get all column names
	columnSet := make(map[string]bool)
	for _, record := range records {
		for key := range record {
			columnSet[key] = true
		}
	}

	// Sort column names for consistent output
	var columns []string
	for col := range columnSet {
		columns = append(columns, col)
	}
	sort.Strings(columns)

	// Create table writer
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	
	// Print header
	for i, col := range columns {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, strings.ToUpper(col))
	}
	fmt.Fprintln(w)

	// Print separator
	for i := range columns {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, strings.Repeat("-", len(columns[i])+2))
	}
	fmt.Fprintln(w)

	// Print rows
	for _, record := range records {
		for i, col := range columns {
			if i > 0 {
				fmt.Fprint(w, "\t")
			}
			value := record[col]
			fmt.Fprint(w, formatValue(value))
		}
		fmt.Fprintln(w)
	}

	fmt.Printf("\n(%d rows)\n", len(records))
	w.Flush()
}

func printCSV(rows []any) {
	if len(rows) == 0 {
		return
	}

	// Convert to []map[string]any
	var records []map[string]any
	for _, row := range rows {
		if record, ok := row.(map[string]any); ok {
			records = append(records, record)
		}
	}

	if len(records) == 0 {
		return
	}

	// Get all column names
	columnSet := make(map[string]bool)
	for _, record := range records {
		for key := range record {
			columnSet[key] = true
		}
	}

	// Sort column names for consistent output
	var columns []string
	for col := range columnSet {
		columns = append(columns, col)
	}
	sort.Strings(columns)

	// Create CSV writer
	writer := csv.NewWriter(os.Stdout)
	defer writer.Flush()

	// Write header
	writer.Write(columns)

	// Write rows
	for _, record := range records {
		var row []string
		for _, col := range columns {
			value := record[col]
			row = append(row, formatValue(value))
		}
		writer.Write(row)
	}
}

func printJSON(data any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.Encode(data)
}

func formatValue(value any) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case int, int32, int64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%.2f", v)
	case bool:
		return strconv.FormatBool(v)
	case time.Time:
		return v.Format("2006-01-02 15:04:05")
	default:
		// For complex types, try JSON marshaling
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
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