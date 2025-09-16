// server/main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
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
)

type Server struct {
	db    *pgxpool.Pool
	llm   openai.Client // value type
	model string
	cache *SchemaCache
	cfg   Config
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
		txt = txt[:schemaMaxChars] + "\n-- …truncated schema…"
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
  'TABLE '||cols.schema||'.'||cols.table||'('||
    string_agg(cols.column||' '||cols.data_type||CASE WHEN cols.is_pk THEN ' PRIMARY KEY' ELSE '' END, ', ' ORDER BY cols.column)||
  ')' AS line
FROM cols
GROUP BY cols.schema, cols.table
UNION ALL
SELECT 'FK '||src_schema||'.'||src_table||'('||src_column||') -> '||dst_schema||'.'||dst_table||'('||dst_column||')'
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
	ttl := defaultSchemaTTL
	if v := os.Getenv("SCHEMA_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ttl = d
		}
	}
	qto := defaultQueryTimeout
	if v := os.Getenv("QUERY_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			qto = d
		}
	}
	mr := defaultMaxRows
	if v := os.Getenv("MAX_ROWS"); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &mr); n == 1 && err == nil && mr > 0 {
		}
	}
	return Config{
		DatabaseURL: envOrDie("DATABASE_URL"),
		OpenAIKey:   os.Getenv("OPENAI_API_KEY"),
		OpenAIModel: envDefault("OPENAI_MODEL", "gpt-4o-mini"),
		OpenAIBase:  os.Getenv("OPENAI_BASE_URL"),
		SchemaTTL:   ttl,
		QueryTO:     qto,
		MaxRows:     mr,
	}
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

// ---------- MCP tool handlers ----------

type askInput struct {
	Query   string `json:"query"`
	MaxRows int    `json:"max_rows,omitempty"`
	DryRun  bool   `json:"dry_run,omitempty"`
}

type askOutput struct {
	SQL  string           `json:"sql"`
	Rows []map[string]any `json:"rows,omitempty"`
	Note string           `json:"note,omitempty"`
}

func (s *Server) handleAsk(ctx context.Context, _ *mcp.CallToolRequest, in askInput) (*mcp.CallToolResult, askOutput, error) {
	start := time.Now()
	log.Debug().Str("tool", "ask").Str("query", strings.TrimSpace(in.Query)).
		Int("max_rows", in.MaxRows).Bool("dry_run", in.DryRun).Msg("request")

	if strings.TrimSpace(in.Query) == "" {
		err := errors.New("query is empty")
		log.Debug().Str("tool", "ask").Err(err).Msg("reject")
		return nil, askOutput{}, err
	}
	schemaTxt, err := s.cache.Get(ctx, s.db)
	if err != nil {
		log.Debug().Str("tool", "ask").Err(err).Msg("schema load failed")
		return nil, askOutput{}, err
	}
	sql, note, err := s.generateSQL(ctx, in.Query, schemaTxt, minNonZero(in.MaxRows, s.cfg.MaxRows))
	if err != nil {
		log.Debug().Str("tool", "ask").Err(err).Msg("sql generation failed")
		return nil, askOutput{}, err
	}
	log.Debug().Str("tool", "ask").Str("sql", sql).Msg("generated sql")

	if in.DryRun {
		if err := guardReadOnly(sql); err != nil {
			log.Debug().Str("tool", "ask").Err(err).Dur("dur", time.Since(start)).Msg("dry-run guard failed")
			return nil, askOutput{SQL: sql, Note: note}, err
		}
		log.Debug().Str("tool", "ask").Dur("dur", time.Since(start)).Msg("dry-run ok")
		return nil, askOutput{SQL: sql, Note: note}, nil
	}
	if err := guardReadOnly(sql); err != nil {
		log.Debug().Str("tool", "ask").Err(err).Dur("dur", time.Since(start)).Msg("guard failed")
		return nil, askOutput{SQL: sql}, err
	}
	rows, err := s.runReadOnlyQuery(ctx, sql, minNonZero(in.MaxRows, s.cfg.MaxRows))
	if err != nil {
		log.Debug().Str("tool", "ask").Err(err).Dur("dur", time.Since(start)).Msg("query failed")
		return nil, askOutput{SQL: sql}, err
	}
	log.Debug().Str("tool", "ask").Int("row_count", len(rows)).Dur("dur", time.Since(start)).Msg("done")
	return nil, askOutput{SQL: sql, Rows: rows, Note: note}, nil
}

type searchInput struct {
	Q     string `json:"q"`
	Limit int    `json:"limit,omitempty"`
}

type searchOutput struct {
	SQL  string           `json:"sql"`
	Rows []map[string]any `json:"rows"`
}

func (s *Server) handleSearch(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, searchOutput, error) {
	start := time.Now()
	log.Debug().Str("tool", "search").Str("q", strings.TrimSpace(in.Q)).Int("limit", in.Limit).Msg("request")

	if strings.TrimSpace(in.Q) == "" {
		err := errors.New("q is empty")
		log.Debug().Str("tool", "search").Err(err).Msg("reject")
		return nil, searchOutput{}, err
	}
	limit := minNonZero(in.Limit, 50)
	sql, err := s.buildSearchSQL(ctx, in.Q, limit)
	if err != nil {
		log.Debug().Str("tool", "search").Err(err).Msg("build sql failed")
		return nil, searchOutput{}, err
	}
	log.Debug().Str("tool", "search").Str("sql", sql).Msg("generated sql")

	rows, err := s.runReadOnlyQuery(ctx, sql, limit)
	if err != nil {
		log.Debug().Str("tool", "search").Err(err).Dur("dur", time.Since(start)).Msg("query failed")
		return nil, searchOutput{SQL: sql}, err
	}
	log.Debug().Str("tool", "search").Int("row_count", len(rows)).Dur("dur", time.Since(start)).Msg("done")
	return nil, searchOutput{SQL: sql, Rows: rows}, nil
}

// ---------- SQL helpers ----------

var mutating = regexp.MustCompile(`(?is)\b(INSERT|UPDATE|DELETE|UPSERT|MERGE|ALTER|DROP|TRUNCATE|VACUUM|REINDEX|GRANT|REVOKE|CREATE)\b`)

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
	sys := `You translate plain English questions into a SINGLE, safe PostgreSQL query.
	Rules:
	- Use only read-only SQL (WITH/SELECT). No writes, DDL, or side effects.
	- Prefer obvious FK joins. Return concise columns with aliases.
	- Always include an explicit LIMIT <= ` + fmt.Sprint(maxRows) + `.
	- Do not add semicolons.
	- Never use placeholder literals like 'specific item' or 'some user'.
	  If the item/user isn’t specified, infer reasonably:
	  • for "specific item" with no ID/sku/title: pick the most recently purchased item.
	  • for "last user": compute it from latest order timestamps.
	- If multiple interpretations are plausible, choose the simplest and document via column aliases.
	
	Schema summary:
	` + schema

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
	impl := &mcp.Implementation{Name: "pgmcp-go", Version: "0.2.0"}

	server := mcp.NewServer(impl, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ask",
		Description: "Answer questions about the connected PostgreSQL database by generating safe, read-only SQL.",
	}, srv.handleAsk)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search",
		Description: "Search free text across all tables/columns (ILIKE).",
	}, srv.handleSearch)

	// --- HTTP + SSE transport ---
	addr := envDefault("HTTP_ADDR", ":8080")
	path := envDefault("HTTP_PATH", "/mcp/sse")
	bearer := strings.TrimSpace(os.Getenv("AUTH_BEARER"))

	base := mcp.NewSSEHandler(func(r *http.Request) *mcp.Server { return server })
	var handler http.Handler = base
	if bearer != "" {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if strings.TrimSpace(got) != bearer {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized"))
				return
			}
			base.ServeHTTP(w, r)
		})
	}

	mux := http.NewServeMux()
	mux.Handle(path, handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("ok")) })

	log.Info().Str("addr", addr).Str("path", path).Msg("starting MCP server on HTTP SSE")
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal().Err(err).Msg("server stopped")
	}
}
