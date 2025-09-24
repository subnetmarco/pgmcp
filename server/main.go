// server/main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	openai "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	defaultSchemaTTL    = 5 * time.Minute
	defaultQueryTimeout = 25 * time.Second
	defaultMaxRows      = 200
	maxModelTokens      = 2000
	schemaMaxChars      = 18000
	maxRequestSize      = 1024 * 1024 // 1MB max request size
	maxQueryLength      = 10000       // Max query length in characters
	pageSize            = 50          // Default page size for pagination
	maxPagesAuto        = 10          // Max pages to auto-fetch
)

type Server struct {
	db     *pgxpool.Pool
	llm    openai.Client // value type
	model  string
	cache  *SchemaCache
	cfg    Config
	server *http.Server
}

type Config struct {
	DatabaseURL string
	OpenAIKey   string
	OpenAIModel string
	OpenAIBase  string
	SchemaTTL   time.Duration
	QueryTO     time.Duration
	MaxRows     int
}

// Validate checks if the configuration is valid and returns detailed errors
func (c *Config) Validate() error {
	var errs []string

	if c.DatabaseURL == "" {
		errs = append(errs, "DATABASE_URL is required")
	}

	if c.MaxRows <= 0 {
		errs = append(errs, "MAX_ROWS must be greater than 0")
	} else if c.MaxRows > 10000 {
		errs = append(errs, "MAX_ROWS cannot exceed 10000 (too many rows could cause memory issues)")
	}

	if c.QueryTO < time.Second {
		errs = append(errs, "QUERY_TIMEOUT must be at least 1 second")
	} else if c.QueryTO > 5*time.Minute {
		errs = append(errs, "QUERY_TIMEOUT cannot exceed 5 minutes")
	}

	if c.SchemaTTL < 30*time.Second {
		errs = append(errs, "SCHEMA_TTL must be at least 30 seconds")
	} else if c.SchemaTTL > 24*time.Hour {
		errs = append(errs, "SCHEMA_TTL cannot exceed 24 hours")
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

type SchemaCache struct {
	mu        sync.RWMutex
	txt       string
	expiresAt time.Time
	ttl       time.Duration
}

func (c *SchemaCache) Get(ctx context.Context, db *pgxpool.Pool) (string, error) {
	c.mu.RLock()
	if time.Now().Before(c.expiresAt) && c.txt != "" {
		defer c.mu.RUnlock()
		return c.txt, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expiresAt) && c.txt != "" {
		return c.txt, nil
	}
	txt, err := loadSchema(ctx, db)
	if err != nil {
		return "", err
	}
	if len(txt) > schemaMaxChars {
		txt = txt[:schemaMaxChars] + "\n-- ...truncated schema..."
	}
	c.txt = txt
	c.expiresAt = time.Now().Add(c.ttl)
	return c.txt, nil
}

func loadSchema(ctx context.Context, db *pgxpool.Pool) (string, error) {
	q := `
WITH cols AS (
  SELECT n.nspname AS schema, c.relname AS table, a.attname AS column,
         pg_catalog.format_type(a.atttypid, a.atttypmod) AS data_type,
         (SELECT EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conrelid = c.oid AND contype='p' AND a.attnum = ANY(conkey)
         )) AS is_pk
  FROM pg_attribute a
  JOIN pg_class c ON a.attrelid = c.oid
  JOIN pg_namespace n ON c.relnamespace = n.oid
  WHERE a.attnum > 0 AND NOT a.attisdropped AND c.relkind='r' AND n.nspname NOT IN ('pg_catalog','information_schema')
),
fks AS (
  SELECT
    n1.nspname AS src_schema, c1.relname AS src_table, a1.attname AS src_column,
    n2.nspname AS dst_schema, c2.relname AS dst_table, a2.attname AS dst_column
  FROM pg_constraint co
  JOIN pg_class c1 ON co.conrelid=c1.oid
  JOIN pg_namespace n1 ON c1.relnamespace=n1.oid
  JOIN pg_class c2 ON co.confrelid=c2.oid
  JOIN pg_namespace n2 ON c2.relnamespace=n2.oid
  JOIN unnest(co.conkey) WITH ORDINALITY AS ck(attnum, pos) ON TRUE
  JOIN unnest(co.confkey) WITH ORDINALITY AS fk(attnum, pos) ON ck.pos=fk.pos
  JOIN pg_attribute a1 ON a1.attrelid=c1.oid AND a1.attnum=ck.attnum
  JOIN pg_attribute a2 ON a2.attrelid=c2.oid AND a2.attnum=fk.attnum
  WHERE co.contype='f'
)
SELECT
  'TABLE '||cols.schema||'.'||
  CASE 
    WHEN cols.table ~ '^[a-z_][a-z0-9_]*$' THEN cols.table
    ELSE '"' || cols.table || '"'
  END ||'('||
    string_agg(
      CASE 
        WHEN cols.column ~ '^[a-z_][a-z0-9_]*$' THEN cols.column
        ELSE '"' || cols.column || '"'
      END ||' '||cols.data_type||CASE WHEN cols.is_pk THEN ' PRIMARY KEY' ELSE '' END, 
      ', ' ORDER BY cols.column
    )||
  ')' AS line
FROM cols
GROUP BY cols.schema, cols.table
UNION ALL
SELECT 'FK '||src_schema||'.'||
  CASE 
    WHEN src_table ~ '^[a-z_][a-z0-9_]*$' THEN src_table
    ELSE '"' || src_table || '"'
  END ||'('||
  CASE 
    WHEN src_column ~ '^[a-z_][a-z0-9_]*$' THEN src_column
    ELSE '"' || src_column || '"'
  END ||') -> '||dst_schema||'.'||
  CASE 
    WHEN dst_table ~ '^[a-z_][a-z0-9_]*$' THEN dst_table
    ELSE '"' || dst_table || '"'
  END ||'('||
  CASE 
    WHEN dst_column ~ '^[a-z_][a-z0-9_]*$' THEN dst_column
    ELSE '"' || dst_column || '"'
  END ||')'
FROM fks
ORDER BY 1;`

	ctxTO, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	rows, err := db.Query(ctxTO, q)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), rows.Err()
}

func mustConfig() Config {
	var warnings []string

	ttl := defaultSchemaTTL
	if v := os.Getenv("SCHEMA_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		} else {
			warnings = append(warnings, fmt.Sprintf("invalid SCHEMA_TTL '%s': %v, using default %v", v, err, defaultSchemaTTL))
		}
	}

	qto := defaultQueryTimeout
	if v := os.Getenv("QUERY_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			qto = d
		} else {
			warnings = append(warnings, fmt.Sprintf("invalid QUERY_TIMEOUT '%s': %v, using default %v", v, err, defaultQueryTimeout))
		}
	}

	mr := defaultMaxRows
	if v := os.Getenv("MAX_ROWS"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &mr); n == 1 && err == nil && mr > 0 {
			// Successfully parsed
		} else {
			warnings = append(warnings, fmt.Sprintf("invalid MAX_ROWS '%s': must be a positive integer, using default %d", v, defaultMaxRows))
			mr = defaultMaxRows
		}
	}

	cfg := Config{
		DatabaseURL: envOrDie("DATABASE_URL"),
		OpenAIKey:   os.Getenv("OPENAI_API_KEY"),
		OpenAIModel: envDefault("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIBase:  os.Getenv("OPENAI_BASE_URL"),
		SchemaTTL:   ttl,
		QueryTO:     qto,
		MaxRows:     mr,
	}

	// Print warnings
	for _, warning := range warnings {
		log.Warn().Msg(warning)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	return cfg
}

func envOrDie(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatal().Msgf("missing required env %s", k)
	}
	return v
}
func envDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func newServer(ctx context.Context, cfg Config) (*Server, error) {
	conf, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	conf.MinConns = 2
	conf.MaxConns = 8
	conf.MaxConnLifetime = 30 * time.Minute
	conf.MaxConnIdleTime = 5 * time.Minute
	conf.HealthCheckPeriod = 30 * time.Second
	conf.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	db, err := pgxpool.NewWithConfig(ctx, conf)
	if err != nil {
		return nil, err
	}

	// Test the connection to ensure it's valid
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}

	var opts []option.RequestOption
	if cfg.OpenAIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.OpenAIKey))
	}
	if cfg.OpenAIBase != "" {
		opts = append(opts, option.WithBaseURL(cfg.OpenAIBase))
	}
	llm := openai.NewClient(opts...) // value

	return &Server{
		db:    db,
		llm:   llm,
		model: cfg.OpenAIModel,
		cache: &SchemaCache{ttl: cfg.SchemaTTL},
		cfg:   cfg,
	}, nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info().Msg("shutting down server gracefully")

	var errs []error

	// Shutdown HTTP server
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("HTTP server shutdown error: %w", err))
		}
	}

	// Close database connections
	if s.db != nil {
		s.db.Close()
		log.Info().Msg("database connections closed")
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	log.Info().Msg("server shutdown complete")
	return nil
}

// ---------- helpers ----------

func minNonZero(v, max int) int {
	if v <= 0 {
		return max
	}
	if v > max {
		return max
	}
	return v
}

// sanitizeInput sanitizes and validates user input
func sanitizeInput(input string) error {
	input = strings.TrimSpace(input)

	if len(input) == 0 {
		return errors.New("input cannot be empty")
	}

	if len(input) > maxQueryLength {
		return fmt.Errorf("input too long: %d characters (max %d)", len(input), maxQueryLength)
	}

	// Check for potentially malicious patterns
	suspicious := []string{
		"--", "/*", "*/", "xp_", "sp_", "exec", "execute",
		"union", "information_schema", "pg_catalog",
	}

	lowerInput := strings.ToLower(input)
	for _, pattern := range suspicious {
		if strings.Contains(lowerInput, pattern) {
			log.Warn().Str("pattern", pattern).Str("input", input).Msg("suspicious pattern detected in input")
		}
	}

	return nil
}

// auditLog logs security-relevant events
func auditLog(event, user, query, result string, success bool) {
	log.Info().
		Str("event", event).
		Str("user", user).
		Str("query", query).
		Str("result", result).
		Bool("success", success).
		Msg("audit_log")
}

// isExpensiveQuery detects potentially expensive query patterns
func isExpensiveQuery(sql string) bool {
	sqlLower := strings.ToLower(sql)

	// Detect expensive patterns (generic)
	expensivePatterns := []string{
		"cross join", // Cartesian products
		"left join",  // LEFT JOINs can be expensive
	}

	for _, pattern := range expensivePatterns {
		if strings.Contains(sqlLower, pattern) {
			return true
		}
	}

	// Count number of JOINs - more than 2 is expensive
	joinCount := strings.Count(sqlLower, " join ")
	return joinCount > 2
}

// simplifyExpensiveQuery rewrites expensive queries to be more performant
func simplifyExpensiveQuery(sql, originalQuery string) string {
	sqlLower := strings.ToLower(sql)

	// For queries with expensive JOINs, return a helpful error message instead
	if strings.Contains(sqlLower, "left join") || strings.Contains(sqlLower, "cross join") || strings.Count(sqlLower, " join ") > 2 {
		return "SELECT 'Query too complex - please try a simpler question or ask about individual tables' AS message LIMIT 1"
	}

	return sql // Return original if no simplification needed
}

func validateSQLBasic(sql string) error {
	sqlLower := strings.ToLower(sql)

	// Check for basic SQL structure
	if !strings.Contains(sqlLower, "select") {
		return fmt.Errorf("query must contain SELECT")
	}

	// Check for balanced parentheses
	openCount := strings.Count(sql, "(")
	closeCount := strings.Count(sql, ")")
	if openCount != closeCount {
		return fmt.Errorf("unbalanced parentheses: %d open, %d close", openCount, closeCount)
	}

	// Check for common syntax issues
	if strings.Contains(sqlLower, "select select") {
		return fmt.Errorf("duplicate SELECT keywords detected")
	}

	return nil
}

// requestSizeLimitMiddleware limits the size of incoming requests
func requestSizeLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
		next.ServeHTTP(w, r)
	})
}

// ---------- MCP tool handlers ----------

type askInput struct {
	Query     string `json:"query"`
	MaxRows   int    `json:"max_rows,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
	Page      int    `json:"page,omitempty"`       // Page number (0-based)
	PageSize  int    `json:"page_size,omitempty"`  // Results per page
	StreamAll bool   `json:"stream_all,omitempty"` // Auto-fetch all pages
}

type askOutput struct {
	SQL        string           `json:"sql"`
	Rows       []map[string]any `json:"rows,omitempty"`
	Note       string           `json:"note,omitempty"`
	Page       int              `json:"page,omitempty"`
	PageSize   int              `json:"page_size,omitempty"`
	TotalCount int              `json:"total_count,omitempty"`
	HasMore    bool             `json:"has_more"`
	NextPage   int              `json:"next_page,omitempty"`
}

type streamInput struct {
	Query    string `json:"query"`
	MaxPages int    `json:"max_pages,omitempty"` // Max pages to fetch (default 10)
	PageSize int    `json:"page_size,omitempty"` // Results per page (default 50)
}

type streamOutput struct {
	SQL        string             `json:"sql"`
	Pages      []streamPageOutput `json:"pages"`
	TotalRows  int                `json:"total_rows"`
	TotalPages int                `json:"total_pages"`
	Note       string             `json:"note,omitempty"`
}

type streamPageOutput struct {
	Page int              `json:"page"`
	Rows []map[string]any `json:"rows"`
}

func (s *Server) handleAsk(ctx context.Context, req *mcp.CallToolRequest, in askInput) (*mcp.CallToolResult, askOutput, error) {
	start := time.Now()
	clientIP := "unknown" // MCP doesn't expose client IP directly

	log.Debug().Str("tool", "ask").Str("query", strings.TrimSpace(in.Query)).
		Int("max_rows", in.MaxRows).Bool("dry_run", in.DryRun).Int("page", in.Page).
		Int("page_size", in.PageSize).Bool("stream_all", in.StreamAll).Str("client_ip", clientIP).Msg("request")

	// Input sanitization and validation
	if err := sanitizeInput(in.Query); err != nil {
		auditLog("ask_input_validation_failed", clientIP, in.Query, err.Error(), false)
		log.Debug().Str("tool", "ask").Err(err).Msg("input validation failed")
		return nil, askOutput{}, err
	}

	schemaTxt, err := s.cache.Get(ctx, s.db)
	if err != nil {
		log.Debug().Str("tool", "ask").Err(err).Msg("schema load failed")
		return nil, askOutput{}, err
	}

	// Determine page size
	pageSize := minNonZero(in.PageSize, pageSize)
	if in.MaxRows > 0 {
		pageSize = minNonZero(in.MaxRows, pageSize)
	}

	sql, note, err := s.generateSQL(ctx, in.Query, schemaTxt, pageSize*10) // Generate SQL for larger limit
	if err != nil {
		auditLog("ask_sql_generation_failed", clientIP, in.Query, err.Error(), false)
		log.Debug().Str("tool", "ask").Err(err).Msg("sql generation failed")
		return nil, askOutput{}, err
	}
	log.Debug().Str("tool", "ask").Str("sql", sql).Msg("generated sql")

	if in.DryRun {
		if err := guardReadOnly(sql); err != nil {
			auditLog("ask_dry_run_guard_failed", clientIP, sql, err.Error(), false)
			log.Debug().Str("tool", "ask").Err(err).Dur("dur", time.Since(start)).Msg("dry-run guard failed")
			return nil, askOutput{SQL: sql, Note: note}, err
		}
		auditLog("ask_dry_run_success", clientIP, in.Query, sql, true)
		log.Debug().Str("tool", "ask").Dur("dur", time.Since(start)).Msg("dry-run ok")
		return nil, askOutput{SQL: sql, Note: note}, nil
	}

	if err := guardReadOnly(sql); err != nil {
		auditLog("ask_guard_failed", clientIP, sql, err.Error(), false)
		log.Debug().Str("tool", "ask").Err(err).Dur("dur", time.Since(start)).Msg("guard failed")
		return nil, askOutput{SQL: sql}, err
	}

	// Check for potentially expensive queries and simplify them
	if isExpensiveQuery(sql) {
		log.Warn().Str("sql", sql).Msg("potentially expensive query detected - simplifying")
		sql = simplifyExpensiveQuery(sql, in.Query)
		log.Info().Str("simplified_sql", sql).Msg("query simplified for performance")
	}

	// Validate SQL syntax before execution (basic check)
	if err := validateSQLBasic(sql); err != nil {
		log.Warn().Str("sql", sql).Err(err).Msg("generated SQL may have issues")
		// Continue anyway - let the database provide the real error
	}

	// Automatically stream all results
	maxPages := 20 // Auto-stream up to 20 pages (1000 results with default page size)
	if in.MaxRows > 0 {
		maxPages = (in.MaxRows + pageSize - 1) / pageSize // Calculate pages needed
	}

	pages, totalRows, err := s.runStreamingQuery(ctx, sql, maxPages, pageSize)
	if err != nil {
		// If query failed due to column errors, try to provide a helpful response
		if strings.Contains(err.Error(), "column") && strings.Contains(err.Error(), "does not exist") {
			auditLog("ask_query_failed", clientIP, sql, err.Error(), false)
			log.Debug().Str("tool", "ask").Err(err).Dur("dur", time.Since(start)).Msg("query failed - column not found")

			// Return helpful error message instead of failing
			errorRows := []map[string]any{
				{
					"error":        "Column not found in generated query",
					"suggestion":   "Try rephrasing your question or ask about specific tables",
					"original_sql": sql,
				},
			}
			return nil, askOutput{
				SQL:  sql,
				Rows: errorRows,
				Note: note + " (query failed - column not found)",
			}, nil
		}

		// Handle table/relation errors gracefully
		if strings.Contains(err.Error(), "relation") && strings.Contains(err.Error(), "does not exist") {
			auditLog("ask_query_failed", clientIP, sql, err.Error(), false)
			log.Debug().Str("tool", "ask").Err(err).Dur("dur", time.Since(start)).Msg("query failed - table not found")

			errorRows := []map[string]any{
				{
					"error":        "Table not found in generated query",
					"suggestion":   "Check available tables or rephrase your question",
					"original_sql": sql,
				},
			}
			return nil, askOutput{
				SQL:  sql,
				Rows: errorRows,
				Note: note + " (query failed - table not found)",
			}, nil
		}

		// Handle syntax errors gracefully
		if strings.Contains(err.Error(), "syntax error") {
			auditLog("ask_query_failed", clientIP, sql, err.Error(), false)
			log.Debug().Str("tool", "ask").Err(err).Dur("dur", time.Since(start)).Msg("query failed - syntax error")

			errorRows := []map[string]any{
				{
					"error":        "SQL syntax error in generated query",
					"suggestion":   "Try rephrasing your question more clearly",
					"original_sql": sql,
				},
			}
			return nil, askOutput{
				SQL:  sql,
				Rows: errorRows,
				Note: note + " (query failed - syntax error)",
			}, nil
		}

		auditLog("ask_query_failed", clientIP, sql, err.Error(), false)
		log.Debug().Str("tool", "ask").Err(err).Dur("dur", time.Since(start)).Msg("query failed")
		return nil, askOutput{SQL: sql}, err
	}

	// Flatten all pages into single result
	var allRows []map[string]any
	for _, page := range pages {
		allRows = append(allRows, page.Rows...)
	}

	auditLog("ask_success", clientIP, in.Query, fmt.Sprintf("streamed %d rows across %d pages", totalRows, len(pages)), true)
	log.Debug().Str("tool", "ask").Int("total_rows", totalRows).Int("pages", len(pages)).
		Int("returned_rows", len(allRows)).Dur("dur", time.Since(start)).Msg("done")

	return nil, askOutput{
		SQL:  sql,
		Rows: allRows,
		Note: fmt.Sprintf("%s (streamed %d pages)", note, len(pages)),
	}, nil
}

type searchInput struct {
	Q     string `json:"q"`
	Limit int    `json:"limit,omitempty"`
}

type searchOutput struct {
	SQL  string           `json:"sql"`
	Rows []map[string]any `json:"rows"`
}

func (s *Server) handleSearch(ctx context.Context, req *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, searchOutput, error) {
	start := time.Now()
	clientIP := "unknown" // MCP doesn't expose client IP directly

	log.Debug().Str("tool", "search").Str("q", strings.TrimSpace(in.Q)).Int("limit", in.Limit).Str("client_ip", clientIP).Msg("request")

	// Input sanitization and validation
	if err := sanitizeInput(in.Q); err != nil {
		auditLog("search_input_validation_failed", clientIP, in.Q, err.Error(), false)
		log.Debug().Str("tool", "search").Err(err).Msg("input validation failed")
		return nil, searchOutput{}, err
	}
	limit := minNonZero(in.Limit, 50)
	sql, err := s.buildSearchSQL(ctx, in.Q, limit)
	if err != nil {
		auditLog("search_sql_build_failed", clientIP, in.Q, err.Error(), false)
		log.Debug().Str("tool", "search").Err(err).Msg("build sql failed")
		return nil, searchOutput{}, err
	}
	log.Debug().Str("tool", "search").Str("sql", sql).Msg("generated sql")

	rows, err := s.runReadOnlyQuery(ctx, sql, limit)
	if err != nil {
		auditLog("search_query_failed", clientIP, sql, err.Error(), false)
		log.Debug().Str("tool", "search").Err(err).Dur("dur", time.Since(start)).Msg("query failed")
		return nil, searchOutput{SQL: sql}, err
	}
	auditLog("search_success", clientIP, in.Q, fmt.Sprintf("returned %d rows", len(rows)), true)
	log.Debug().Str("tool", "search").Int("row_count", len(rows)).Dur("dur", time.Since(start)).Msg("done")
	return nil, searchOutput{SQL: sql, Rows: rows}, nil
}

func (s *Server) handleStream(ctx context.Context, req *mcp.CallToolRequest, in streamInput) (*mcp.CallToolResult, streamOutput, error) {
	start := time.Now()
	clientIP := "unknown"

	log.Debug().Str("tool", "stream").Str("query", strings.TrimSpace(in.Query)).
		Int("max_pages", in.MaxPages).Int("page_size", in.PageSize).Str("client_ip", clientIP).Msg("request")

	// Input sanitization and validation
	if err := sanitizeInput(in.Query); err != nil {
		auditLog("stream_input_validation_failed", clientIP, in.Query, err.Error(), false)
		log.Debug().Str("tool", "stream").Err(err).Msg("input validation failed")
		return nil, streamOutput{}, err
	}

	schemaTxt, err := s.cache.Get(ctx, s.db)
	if err != nil {
		log.Debug().Str("tool", "stream").Err(err).Msg("schema load failed")
		return nil, streamOutput{}, err
	}

	// Parameters
	maxPages := minNonZero(in.MaxPages, maxPagesAuto)
	pageSize := minNonZero(in.PageSize, pageSize)

	sql, note, err := s.generateSQL(ctx, in.Query, schemaTxt, pageSize*maxPages)
	if err != nil {
		auditLog("stream_sql_generation_failed", clientIP, in.Query, err.Error(), false)
		log.Debug().Str("tool", "stream").Err(err).Msg("sql generation failed")
		return nil, streamOutput{}, err
	}

	if err := guardReadOnly(sql); err != nil {
		auditLog("stream_guard_failed", clientIP, sql, err.Error(), false)
		log.Debug().Str("tool", "stream").Err(err).Msg("guard failed")
		return nil, streamOutput{SQL: sql}, err
	}

	// Get all pages
	pages, totalRows, err := s.runStreamingQuery(ctx, sql, maxPages, pageSize)
	if err != nil {
		auditLog("stream_query_failed", clientIP, sql, err.Error(), false)
		log.Debug().Str("tool", "stream").Err(err).Dur("dur", time.Since(start)).Msg("query failed")
		return nil, streamOutput{SQL: sql}, err
	}

	totalPages := (totalRows + pageSize - 1) / pageSize // Ceiling division

	auditLog("stream_success", clientIP, in.Query, fmt.Sprintf("returned %d rows in %d pages", totalRows, len(pages)), true)
	log.Debug().Str("tool", "stream").Int("total_rows", totalRows).Int("pages", len(pages)).
		Dur("dur", time.Since(start)).Msg("done")

	return nil, streamOutput{
		SQL:        sql,
		Pages:      pages,
		TotalRows:  totalRows,
		TotalPages: totalPages,
		Note:       note,
	}, nil
}

// ---------- SQL helpers ----------

var mutating = regexp.MustCompile(`(?is)\b(INSERT|UPDATE|DELETE|UPSERT|MERGE|ALTER|DROP|TRUNCATE|VACUUM|REINDEX|GRANT|REVOKE|CREATE|COPY|ROLLBACK|COMMIT|BEGIN|START|SAVEPOINT|RELEASE|SET)\b`)

func guardReadOnly(sql string) error {
	if mutating.MatchString(sql) {
		return fmt.Errorf("refusing to run non-read-only SQL")
	}
	// disallow multiple statements
	if strings.Count(sql, ";") > 0 && strings.TrimSpace(sql)[len(strings.TrimSpace(sql))-1] != ';' {
		return fmt.Errorf("multiple statements not allowed")
	}
	return nil
}

func (s *Server) runReadOnlyQuery(ctx context.Context, sql string, limit int) ([]map[string]any, error) {
	ctxTO, cancel := context.WithTimeout(ctx, s.cfg.QueryTO)
	defer cancel()
	conn, err := s.db.Acquire(ctxTO)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctxTO, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctxTO)

	if !regexp.MustCompile(`(?is)\bLIMIT\s+\d+`).MatchString(sql) {
		sql = fmt.Sprintf("WITH q AS (%s) SELECT * FROM q LIMIT %d", sql, limit)
	}
	rows, err := tx.Query(ctxTO, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	flds := rows.FieldDescriptions()
	out := make([]map[string]any, 0, 16)
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(flds))
		for i, f := range flds {
			row[string(f.Name)] = vals[i]
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctxTO); err != nil {
		return nil, err
	}
	return out, nil
}

// PaginatedResult holds pagination information
type PaginatedResult struct {
	Rows       []map[string]any
	Page       int
	PageSize   int
	TotalCount int
	HasMore    bool
	NextPage   int
}

func (s *Server) runPaginatedQuery(ctx context.Context, sql string, page, pageSize int) (*PaginatedResult, error) {
	// First, get total count
	countSQL := fmt.Sprintf("WITH query AS (%s) SELECT COUNT(*) FROM query", sql)

	ctxTO, cancel := context.WithTimeout(ctx, s.cfg.QueryTO)
	defer cancel()

	conn, err := s.db.Acquire(ctxTO)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctxTO, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctxTO)

	var totalCount int
	if err := tx.QueryRow(ctxTO, countSQL).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get paginated data
	offset := page * pageSize
	paginatedSQL := fmt.Sprintf("WITH query AS (%s) SELECT * FROM query LIMIT %d OFFSET %d", sql, pageSize, offset)

	rows, err := tx.Query(ctxTO, paginatedSQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	flds := rows.FieldDescriptions()
	out := make([]map[string]any, 0, pageSize)
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(flds))
		for i, f := range flds {
			row[string(f.Name)] = vals[i]
		}
		out = append(out, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctxTO); err != nil {
		return nil, err
	}

	hasMore := offset+len(out) < totalCount
	nextPage := page + 1
	if !hasMore {
		nextPage = 0
	}

	return &PaginatedResult{
		Rows:       out,
		Page:       page,
		PageSize:   pageSize,
		TotalCount: totalCount,
		HasMore:    hasMore,
		NextPage:   nextPage,
	}, nil
}

func (s *Server) runStreamingQuery(ctx context.Context, sql string, maxPages, pageSize int) ([]streamPageOutput, int, error) {
	// First, get total count
	countSQL := fmt.Sprintf("WITH query AS (%s) SELECT COUNT(*) FROM query", sql)

	ctxTO, cancel := context.WithTimeout(ctx, s.cfg.QueryTO*time.Duration(maxPages))
	defer cancel()

	conn, err := s.db.Acquire(ctxTO)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctxTO, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctxTO)

	var totalCount int
	if err := tx.QueryRow(ctxTO, countSQL).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// Calculate actual pages to fetch
	totalPages := (totalCount + pageSize - 1) / pageSize
	pagesToFetch := minNonZero(maxPages, totalPages)

	var pages []streamPageOutput

	// Fetch pages
	for page := 0; page < pagesToFetch; page++ {
		offset := page * pageSize
		paginatedSQL := fmt.Sprintf("WITH query AS (%s) SELECT * FROM query LIMIT %d OFFSET %d", sql, pageSize, offset)

		rows, err := tx.Query(ctxTO, paginatedSQL)
		if err != nil {
			return nil, 0, err
		}

		flds := rows.FieldDescriptions()
		pageRows := make([]map[string]any, 0, pageSize)
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				rows.Close()
				return nil, 0, err
			}
			row := make(map[string]any, len(flds))
			for i, f := range flds {
				row[string(f.Name)] = vals[i]
			}
			pageRows = append(pageRows, row)
		}
		rows.Close()

		if err := rows.Err(); err != nil {
			return nil, 0, err
		}

		pages = append(pages, streamPageOutput{
			Page: page,
			Rows: pageRows,
		})

		// Stop if no more rows
		if len(pageRows) < pageSize {
			break
		}
	}

	if err := tx.Commit(ctxTO); err != nil {
		return nil, 0, err
	}

	return pages, totalCount, nil
}

func (s *Server) buildSearchSQL(ctx context.Context, q string, limit int) (string, error) {
	const meta = `
SELECT table_schema, table_name, column_name
FROM information_schema.columns
WHERE data_type IN ('text','character varying','character','citext')
  AND table_schema NOT IN ('pg_catalog','information_schema')
ORDER BY table_schema, table_name, ordinal_position;
`
	ctxTO, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows, err := s.db.Query(ctxTO, meta)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type col struct{ s, t, c string }
	var cols []col
	for rows.Next() {
		var c col
		if err := rows.Scan(&c.s, &c.t, &c.c); err != nil {
			return "", err
		}
		cols = append(cols, c)
	}
	if len(cols) == 0 {
		return "", errors.New("no searchable columns")
	}
	like := strings.ReplaceAll(q, "'", "''")
	var parts []string
	for _, c := range cols {
		parts = append(parts, fmt.Sprintf(
			`SELECT '%s.%s' AS source_table, '%s' AS column, LEFT(CAST("%s" AS text), 240) AS match_text FROM "%s"."%s" WHERE "%s" ILIKE '%%%s%%'`,
			c.s, c.t, c.c, c.c, c.s, c.t, c.c, like,
		))
		if len(parts) >= 60 {
			break
		}
	}
	sql := "WITH u AS (\n" + strings.Join(parts, "\nUNION ALL\n") + fmt.Sprintf("\n) SELECT * FROM u LIMIT %d", limit)
	return sql, nil
}

func (s *Server) generateSQL(ctx context.Context, question, schema string, maxRows int) (string, string, error) {
	sys := `You translate plain English questions into a SINGLE, safe PostgreSQL query for ANY PostgreSQL database.

	Core Rules:
	- Use only read-only SQL (WITH/SELECT). No writes, DDL, or side effects.
	- Use proper JOINs based on foreign key relationships shown in the schema.
	- Always include an explicit LIMIT <= ` + fmt.Sprint(maxRows) + `.
	- Do not add semicolons.
	- Return concise, meaningful column aliases.
	- CRITICAL: Use table and column names EXACTLY as shown in the schema below, including quotes when present.

	Query Scope Rules:
	- SINGULAR questions ("Who is the...", "What is the...") -> LIMIT 1
	- PLURAL questions ("Who are the...", "What are the...") -> LIMIT 20
	- COUNT questions ("How many...") -> Return COUNT, no additional LIMIT
	- LIST questions ("List all...", "Show all...") -> LIMIT 50
	- COMPARISON questions ("Compare X and Y...") -> Return just the compared items
	
	User Override Rules (when user explicitly wants more results):
	- "Show ALL [items]" or "List ALL [items]" -> Use larger LIMIT (200-500)
	- "Give me EVERY [item]" -> Use larger LIMIT (200-500) 
	- "Show me EVERYTHING" -> Use larger LIMIT (200-500)
	- "Complete list of [items]" -> Use larger LIMIT (200-500)
	- When user emphasizes ALL/EVERY/COMPLETE -> Override normal limits
	- But still respect the maximum LIMIT constraint provided

	CRITICAL COLUMN CHECKING RULES:
	- BEFORE writing ANY SQL, verify EVERY column exists in the table you're using
	- ONLY use columns that are explicitly listed in the schema below
	- If you need a column that doesn't exist in your target table, you MUST use JOINs
	- Example: If you need user_id but you're querying order_items (which has no user_id), 
	  you MUST JOIN: order_items -> orders -> users via the foreign keys shown in schema
	- NEVER assume standard columns like 'id' exist - many tables use composite keys
	- For counting records: use COUNT(*) instead of COUNT(table.id) unless 'id' is explicitly shown
	- NEVER write SQL with non-existent columns - this will cause errors

	Universal Data Handling:
	- Work ONLY with tables and columns shown in the schema summary below
	- NEVER assume columns exist - only use columns explicitly listed in the schema
	- NEVER assume specific data values, enum values, or business logic
	- NEVER filter by assumed status values (completed, active, etc.) unless explicitly mentioned
	- If user asks for "top X" or "most Y", aggregate and sort the available data as-is
	- Use column names and relationships exactly as they appear in the schema
	- When in doubt, include more data rather than filtering it out
	- Focus on structural relationships (JOINs) rather than data content assumptions

	CRITICAL Identifier Rules (PostgreSQL Case Sensitivity):
	- PostgreSQL identifiers are case-sensitive when quoted with double quotes
	- Use table and column names EXACTLY as they appear in the schema below
	- If the schema shows "Book" (with quotes), you MUST use "Book" in your SQL
	- If the schema shows book (no quotes), you can use book, Book, or BOOK
	- NEVER change the case or remove quotes from identifiers shown in the schema
	- When in doubt, copy the identifier exactly as shown in the schema

	CRITICAL Counting and Aggregation Rules:
	- For counting records, use COUNT(*) or COUNT(1) instead of COUNT(table.id)
	- Only use COUNT(column_name) if that column is explicitly listed in the schema
	- Many tables use composite primary keys and don't have an 'id' column
	- For aggregating quantities or amounts, use SUM(column_name) where column_name exists
	- Always verify the column exists in the schema before using it in COUNT, SUM, AVG, etc.

	JOIN Strategy (CRITICAL - Generic approach for ANY database):
	- ALWAYS check the schema summary for foreign key relationships before writing JOINs
	- Look for "FK" lines in the schema that show: table1(column1) -> table2(column2)
	- If a column doesn't exist in the target table, trace the foreign key path in the schema
	- Example: If table A has column X, but you need column Y from table B, look for FK A.some_id -> B.id
	- Use ONLY the foreign key relationships explicitly shown in the schema summary
	- When multiple JOIN paths exist, choose the most direct one with fewest tables
	
	Performance Guidelines (CRITICAL):
	- PREFER single-table queries when possible
	- When JOINs are necessary, use ONLY the foreign key relationships explicitly shown in schema
	- Limit JOINs to maximum 2 tables to avoid expensive operations
	- Use INNER JOINs instead of LEFT JOINs when possible
	- If a question requires more than 2 JOINs, simplify to a single-table approximation
	- NEVER assume columns exist in the wrong table - always verify against schema first

	MANDATORY: Study this schema summary carefully before writing SQL. It shows all tables, columns, and foreign key relationships:
	
	` + schema + `
	
	REMEMBER: If you need a column that doesn't exist in your target table, find the FK relationship above and use JOINs.`

	user := "Question: " + strings.TrimSpace(question) + `
Return ONLY SQL, nothing else.`

	ctxTO, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()

	resp, err := s.llm.Chat.Completions.New(ctxTO, openai.ChatCompletionNewParams{
		Model: openai.ChatModel(s.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(sys),
			openai.UserMessage(user),
		},
		MaxTokens:   openai.Int(maxModelTokens),
		Temperature: openai.Float(0.2),
	})
	if err != nil {
		return "", "", err
	}
	sql := strings.TrimSpace(resp.Choices[0].Message.Content)
	sql = strings.Trim(sql, "```")
	sql = strings.TrimSpace(strings.TrimPrefix(sql, "sql"))
	note := "model=" + s.model
	return sql, note, nil
}

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	switch strings.ToLower(envDefault("LOG_LEVEL", "info")) {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	cfg := mustConfig()
	ctx := context.Background()

	srv, err := newServer(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("init failed")
	}
	impl := &mcp.Implementation{Name: "pgmcp-go", Version: "0.3.0"}

	server := mcp.NewServer(impl, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ask",
		Description: "Answer questions about the connected PostgreSQL database by generating safe, read-only SQL. Automatically streams all results.",
	}, srv.handleAsk)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Search free text across all tables/columns (ILIKE).",
	}, srv.handleSearch)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "stream",
		Description: "Stream large result sets by automatically fetching all pages. Returns complete results progressively.",
	}, srv.handleStream)

	// --- Streamable HTTP transport ---
	addr := envDefault("HTTP_ADDR", ":8080")
	path := envDefault("HTTP_PATH", "/mcp")
	bearer := strings.TrimSpace(os.Getenv("AUTH_BEARER"))

	base := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)
	var handler http.Handler = base
	if bearer != "" {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if strings.TrimSpace(got) != bearer {
				auditLog("auth_failed", r.RemoteAddr, "", "invalid bearer token", false)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized"))
				return
			}
			base.ServeHTTP(w, r)
		})
	}

	// Apply middleware
	handler = requestSizeLimitMiddleware(handler)

	mux := http.NewServeMux()
	mux.Handle(path, handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	srv.server = httpServer

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info().Msg("received shutdown signal")

		// Give ongoing requests 30 seconds to complete
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("error during shutdown")
		}
		os.Exit(0)
	}()

	log.Info().Str("addr", addr).Str("path", path).Msg("starting MCP server on streamable HTTP")
	auditLog("server_start", "system", "", addr, true)

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("server error")
	}
}
