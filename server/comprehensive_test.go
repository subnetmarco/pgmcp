//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStreamingLargeDataset(t *testing.T) {
	t.Parallel()

	// Setup test database
	db := mustPool(t)
	defer db.Close()
	setupComprehensiveTestData(t, db)

	// Setup mock OpenAI
	llm := mockOpenAIComprehensive(t)
	defer llm.Close()

	// Setup server
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test-key",
		OpenAIBase:  llm.URL + "/v1",
		OpenAIModel: "test-model",
		SchemaTTL:   2 * time.Minute,
		QueryTO:     30 * time.Second,
		MaxRows:     50,
	}

	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
		// Test streaming with large dataset
		pages, totalCount, err := srv.runStreamingQuery(ctx, "SELECT id, name FROM test_users ORDER BY id", 5, 10)
		if err != nil {
			t.Fatalf("runStreamingQuery: %v", err)
		}

		if len(pages) != 5 {
			t.Fatalf("expected 5 pages, got %d", len(pages))
		}

		if totalCount != 100 {
			t.Fatalf("expected 100 total records, got %d", totalCount)
		}

		// Verify page structure
		for i, page := range pages {
			if page.Page != i {
				t.Fatalf("page %d has wrong page number: %d", i, page.Page)
			}
			if len(page.Rows) != 10 {
				t.Fatalf("page %d has wrong row count: %d, expected 10", i, len(page.Rows))
			}
		}
}

func TestPaginationEdgeCases(t *testing.T) {
	t.Parallel()

	// Setup test database
	db := mustPool(t)
	defer db.Close()
	setupComprehensiveTestData(t, db)

	// Setup mock OpenAI
	llm := mockOpenAIComprehensive(t)
	defer llm.Close()

	// Setup server
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test-key",
		OpenAIBase:  llm.URL + "/v1",
		OpenAIModel: "test-model",
		SchemaTTL:   2 * time.Minute,
		QueryTO:     30 * time.Second,
		MaxRows:     50,
	}

	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	// Test pagination with edge cases
	testCases := []struct {
		name            string
		sql             string
		page            int
		pageSize        int
		expectedRows    int
		expectedHasMore bool
	}{
		{
			name:            "first_page",
			sql:             "SELECT id FROM test_users ORDER BY id",
			page:            0,
			pageSize:        10,
			expectedRows:    10,
			expectedHasMore: true,
		},
		{
			name:            "last_page",
			sql:             "SELECT id FROM test_users ORDER BY id",
			page:            9,
			pageSize:        10,
			expectedRows:    10,
			expectedHasMore: false,
		},
		{
			name:            "empty_result",
			sql:             "SELECT id FROM test_users WHERE id > 1000",
			page:            0,
			pageSize:        10,
			expectedRows:    0,
			expectedHasMore: false,
		},
		{
			name:            "single_result",
			sql:             "SELECT id FROM test_users WHERE id = 1",
			page:            0,
			pageSize:        10,
			expectedRows:    1,
			expectedHasMore: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := srv.runPaginatedQuery(ctx, tc.sql, tc.page, tc.pageSize)
			if err != nil {
				t.Fatalf("runPaginatedQuery: %v", err)
			}

			if len(result.Rows) != tc.expectedRows {
				t.Fatalf("expected %d rows, got %d", tc.expectedRows, len(result.Rows))
			}

			if result.HasMore != tc.expectedHasMore {
				t.Fatalf("expected hasMore %v, got %v", tc.expectedHasMore, result.HasMore)
			}
		})
	}
}

func TestSecurityAndValidation(t *testing.T) {
	t.Parallel()

	// Setup test database
	db := mustPool(t)
	defer db.Close()
	setupComprehensiveTestData(t, db)

	// Setup mock OpenAI
	llm := mockOpenAIComprehensive(t)
	defer llm.Close()

	// Setup server
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test-key",
		OpenAIBase:  llm.URL + "/v1",
		OpenAIModel: "test-model",
		SchemaTTL:   2 * time.Minute,
		QueryTO:     30 * time.Second,
		MaxRows:     50,
	}

	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
		// Test input validation
		testCases := []struct {
			name    string
			input   askInput
			wantErr bool
		}{
			{
				name:    "valid_query",
				input:   askInput{Query: "SELECT * FROM test_users"},
				wantErr: false,
			},
			{
				name:    "empty_query",
				input:   askInput{Query: ""},
				wantErr: true,
			},
			{
				name:    "whitespace_query",
				input:   askInput{Query: "   "},
				wantErr: true,
			},
			{
				name:    "too_long_query",
				input:   askInput{Query: strings.Repeat("SELECT ", 2000)},
				wantErr: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_, _, err := srv.handleAsk(ctx, nil, tc.input)
				hasErr := err != nil

				if hasErr != tc.wantErr {
					t.Fatalf("handleAsk error = %v, wantErr %v", err, tc.wantErr)
				}
			})
		}
}

func TestQueryComplexityProtection(t *testing.T) {
	t.Parallel()

	// Setup test database
	db := mustPool(t)
	defer db.Close()
	setupComprehensiveTestData(t, db)

	// Setup mock OpenAI
	llm := mockOpenAIComprehensive(t)
	defer llm.Close()

	// Setup server
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test-key",
		OpenAIBase:  llm.URL + "/v1",
		OpenAIModel: "test-model",
		SchemaTTL:   2 * time.Minute,
		QueryTO:     30 * time.Second,
		MaxRows:     50,
	}

	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
		// Test that expensive queries are detected and handled
		expensiveQueries := []string{
			"SELECT * FROM users u LEFT JOIN orders o ON u.id = o.user_id LEFT JOIN items i ON o.item_id = i.id",
			"SELECT * FROM users CROSS JOIN orders",
			"SELECT * FROM users u JOIN orders o ON u.id = o.user_id JOIN items i ON o.item_id = i.id JOIN reviews r ON i.id = r.item_id",
		}

		for _, sql := range expensiveQueries {
			t.Run("expensive_query", func(t *testing.T) {
				if !isExpensiveQuery(sql) {
					t.Fatalf("expected query to be detected as expensive: %s", sql)
				}

				simplified := simplifyExpensiveQuery(sql, "test query")
				if simplified == sql {
					t.Fatalf("expected query to be simplified, but it wasn't: %s", sql)
				}

				if !strings.Contains(simplified, "Query too complex") {
					t.Fatalf("expected simplified query to contain error message, got: %s", simplified)
				}
			})
		}
}

func TestConfigurationValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid_production_config",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				OpenAIKey:   "sk-test123",
				OpenAIModel: "gpt-4",
				MaxRows:     1000,
				QueryTO:     30 * time.Second,
				SchemaTTL:   10 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "missing_database_url",
			cfg: Config{
				OpenAIKey: "sk-test123",
				MaxRows:   100,
				QueryTO:   10 * time.Second,
				SchemaTTL: 5 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "invalid_max_rows_zero",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     0,
				QueryTO:     10 * time.Second,
				SchemaTTL:   5 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "invalid_max_rows_too_high",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     50000,
				QueryTO:     10 * time.Second,
				SchemaTTL:   5 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "invalid_query_timeout",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     100,
				QueryTO:     500 * time.Millisecond,
				SchemaTTL:   5 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "invalid_schema_ttl",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     100,
				QueryTO:     10 * time.Second,
				SchemaTTL:   10 * time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuditLogging(t *testing.T) {
	// Test that audit logging doesn't panic and formats correctly
	testCases := []struct {
		event   string
		user    string
		query   string
		result  string
		success bool
	}{
		{"ask_success", "test_user", "SELECT * FROM users", "returned 5 rows", true},
		{"search_success", "test_user", "search term", "found 3 matches", true},
		{"ask_failed", "test_user", "bad query", "syntax error", false},
		{"auth_failed", "192.168.1.1", "", "invalid token", false},
	}

	for _, tc := range testCases {
		t.Run(tc.event, func(t *testing.T) {
			// This should not panic
			auditLog(tc.event, tc.user, tc.query, tc.result, tc.success)
		})
	}
}

func setupComprehensiveTestData(t *testing.T, db *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()

	// Clean up any existing test tables first
	cleanup := `
		DROP TABLE IF EXISTS test_items CASCADE;
		DROP TABLE IF EXISTS test_orders CASCADE;
		DROP TABLE IF EXISTS test_users CASCADE;
	`
	_, err := db.Exec(ctx, cleanup)
	if err != nil {
		t.Fatalf("cleanup test tables: %v", err)
	}

	// Create test tables
	schema := `
		CREATE TABLE test_users (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT UNIQUE NOT NULL,
			created_at TIMESTAMPTZ DEFAULT now()
		);
		
		CREATE TABLE test_orders (
			id SERIAL PRIMARY KEY,
			user_id INT REFERENCES test_users(id),
			total_cents INT NOT NULL,
			status TEXT DEFAULT 'pending',
			created_at TIMESTAMPTZ DEFAULT now()
		);
		
		CREATE TABLE test_items (
			id SERIAL PRIMARY KEY,
			order_id INT REFERENCES test_orders(id),
			name TEXT NOT NULL,
			price_cents INT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT now()
		);
	`

	_, err = db.Exec(ctx, schema)
	if err != nil {
		t.Fatalf("create test schema: %v", err)
	}

	// Clear existing data
	_, err = db.Exec(ctx, "TRUNCATE test_users, test_orders, test_items CASCADE")
	if err != nil {
		t.Fatalf("truncate test tables: %v", err)
	}

	// Insert test data
	for i := 1; i <= 100; i++ {
		_, err = db.Exec(ctx,
			"INSERT INTO test_users (name, email) VALUES ($1, $2)",
			"User "+intToString(i), "user"+intToString(i)+"@test.com")
		if err != nil {
			t.Fatalf("insert test user %d: %v", i, err)
		}
	}

	// Insert orders (some users have multiple orders)
	for i := 1; i <= 50; i++ {
		userID := 1 + (i-1)%25 // First 25 users get orders
		_, err = db.Exec(ctx,
			"INSERT INTO test_orders (user_id, total_cents, status) VALUES ($1, $2, $3)",
			userID, i*100, "completed")
		if err != nil {
			t.Fatalf("insert test order %d: %v", i, err)
		}
	}

	// Insert items
	for i := 1; i <= 100; i++ {
		orderID := 1 + (i-1)%50 // Each order gets 2 items on average
		_, err = db.Exec(ctx,
			"INSERT INTO test_items (order_id, name, price_cents) VALUES ($1, $2, $3)",
			orderID, "Item "+intToString(i), i*50)
		if err != nil {
			t.Fatalf("insert test item %d: %v", i, err)
		}
	}
}

func mockOpenAIComprehensive(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		// Parse request to determine query type
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		bodyStr := strings.ToLower(string(body))

		var sql string
		switch {
		case strings.Contains(bodyStr, "all users"):
			sql = "SELECT id, name, email FROM test_users ORDER BY id LIMIT 100"
		case strings.Contains(bodyStr, "top users"):
			sql = "SELECT id, name, email FROM test_users ORDER BY created_at DESC LIMIT 10"
		case strings.Contains(bodyStr, "count"):
			sql = "SELECT COUNT(*) FROM test_users"
		case strings.Contains(bodyStr, "most orders"):
			sql = "SELECT user_id, COUNT(*) as order_count FROM test_orders GROUP BY user_id ORDER BY order_count DESC LIMIT 1"
		default:
			sql = "SELECT id, name FROM test_users ORDER BY id LIMIT 10"
		}

		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "test",
			"choices": []any{
				map[string]any{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": sql,
					},
					"finish_reason": "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestMCPHandlerIntegration(t *testing.T) {
	t.Parallel()

	// Setup
	db := mustPool(t)
	defer db.Close()
	setupComprehensiveTestData(t, db)

	llm := mockOpenAIComprehensive(t)
	defer llm.Close()

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test-key",
		OpenAIBase:  llm.URL + "/v1",
		OpenAIModel: "test-model",
		SchemaTTL:   2 * time.Minute,
		QueryTO:     10 * time.Second,
		MaxRows:     50,
	}

	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	t.Run("ask_handler_streaming", func(t *testing.T) {
		_, output, err := srv.handleAsk(ctx, nil, askInput{
			Query:   "Show all users",
			MaxRows: 100,
		})
		if err != nil {
			t.Fatalf("handleAsk: %v", err)
		}

		if len(output.Rows) == 0 {
			t.Fatalf("expected results, got none")
		}

		if !strings.Contains(output.Note, "streamed") {
			t.Fatalf("expected note to mention streaming, got: %s", output.Note)
		}
	})

	t.Run("search_handler", func(t *testing.T) {
		_, output, err := srv.handleSearch(ctx, nil, searchInput{
			Q:     "User",
			Limit: 10,
		})
		if err != nil {
			t.Fatalf("handleSearch: %v", err)
		}

		if len(output.Rows) == 0 {
			t.Fatalf("expected search results, got none")
		}
	})

	t.Run("stream_handler", func(t *testing.T) {
		_, output, err := srv.handleStream(ctx, nil, streamInput{
			Query:    "List all users",
			MaxPages: 3,
			PageSize: 10,
		})
		if err != nil {
			t.Fatalf("handleStream: %v", err)
		}

		if len(output.Pages) == 0 {
			t.Fatalf("expected pages, got none")
		}

		if output.TotalRows == 0 {
			t.Fatalf("expected total rows > 0, got %d", output.TotalRows)
		}
	})
}

func BenchmarkQueryPerformance(b *testing.B) {
	// Setup
	db := mustPoolForBench(b)
	defer db.Close()
	createLargeTestDataForBench(b, db, 10000) // 10k records

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		QueryTO:     30 * time.Second,
		MaxRows:     50,
	}

	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		b.Fatalf("newServer: %v", err)
	}

	b.ResetTimer()

	b.Run("simple_query", func(b *testing.B) {
		sql := "SELECT id, name FROM large_test_table ORDER BY id"
		for i := 0; i < b.N; i++ {
			_, err := srv.runReadOnlyQuery(ctx, sql, 50)
			if err != nil {
				b.Fatalf("runReadOnlyQuery: %v", err)
			}
		}
	})

	b.Run("pagination", func(b *testing.B) {
		sql := "SELECT id, name FROM large_test_table ORDER BY id"
		for i := 0; i < b.N; i++ {
			_, err := srv.runPaginatedQuery(ctx, sql, 0, 50)
			if err != nil {
				b.Fatalf("runPaginatedQuery: %v", err)
			}
		}
	})

	b.Run("streaming_3_pages", func(b *testing.B) {
		sql := "SELECT id, name FROM large_test_table ORDER BY id"
		for i := 0; i < b.N; i++ {
			_, _, err := srv.runStreamingQuery(ctx, sql, 3, 50)
			if err != nil {
				b.Fatalf("runStreamingQuery: %v", err)
			}
		}
	})
}

func createLargeTestDataForBench(tb testing.TB, db *pgxpool.Pool, count int) {
	tb.Helper()

	ctx := context.Background()

	// Create test table
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS large_test_table (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			value INT NOT NULL,
			created_at TIMESTAMPTZ DEFAULT now()
		)
	`)
	if err != nil {
		tb.Fatalf("create large test table: %v", err)
	}

	// Clear existing data
	_, err = db.Exec(ctx, "TRUNCATE large_test_table")
	if err != nil {
		tb.Fatalf("truncate large test table: %v", err)
	}

	// Insert test records in batches
	batchSize := 1000
	for i := 0; i < count; i += batchSize {
		end := i + batchSize
		if end > count {
			end = count
		}

		var values []any
		var placeholders []string
		idx := 1

		for j := i; j < end; j++ {
			placeholders = append(placeholders, "($"+intToString(idx)+", $"+intToString(idx+1)+")")
			values = append(values, "Record "+intToString(j+1), (j+1)*10)
			idx += 2
		}

		sql := "INSERT INTO large_test_table (name, value) VALUES " + strings.Join(placeholders, ", ")

		_, err = db.Exec(ctx, sql, values...)
		if err != nil {
			tb.Fatalf("insert batch %d-%d: %v", i, end, err)
		}
	}
}

func mustPoolForBench(b *testing.B) *pgxpool.Pool {
	b.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@127.0.0.1:5432/pgmcp_test?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		b.Skipf("skip: cannot parse DATABASE_URL: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		b.Skipf("skip: cannot connect to postgres: %v", err)
	}
	return pool
}

func TestErrorHandlingIntegration(t *testing.T) {
	t.Parallel()

	// Setup test database
	db := mustPool(t)
	defer db.Close()
	setupComprehensiveTestData(t, db)

	// Setup mock OpenAI that returns bad SQL
	badSQLResponses := map[string]string{
		"user that purchased most items": `SELECT user_id, COUNT(*) FROM order_items GROUP BY user_id ORDER BY COUNT(*) DESC LIMIT 1`, // user_id doesn't exist in order_items
		"show me tables":                 `SELECT * FROM nonexistent_table LIMIT 10`,                                                  // table doesn't exist
		"count all users":                `SELEC COUNT(*) FROM users`,                                                                 // syntax error
	}

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)

		messages := req["messages"].([]interface{})
		userMsg := messages[len(messages)-1].(map[string]interface{})
		question := strings.ToLower(userMsg["content"].(string))

		var sqlResponse string
		for key, sql := range badSQLResponses {
			if strings.Contains(question, key) {
				sqlResponse = sql
				break
			}
		}

		if sqlResponse == "" {
			sqlResponse = "SELECT 1 as result LIMIT 1" // fallback
		}

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": sqlResponse}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer llm.Close()

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test-key",
		OpenAIBase:  llm.URL + "/v1",
		OpenAIModel: "gpt-4o-mini",
		SchemaTTL:   2 * time.Minute,
		QueryTO:     5 * time.Second,
		MaxRows:     50,
	}

	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	// Test column error handling
	t.Run("column_does_not_exist", func(t *testing.T) {
		_, result, err := srv.handleAsk(context.Background(), nil, askInput{
			Query: "Give me the user that purchased most items",
		})

		if err != nil {
			t.Fatalf("Expected graceful error handling, got error: %v", err)
		}

		// Should return error information in rows
		if len(result.Rows) == 0 {
			t.Fatalf("Expected error information in rows, got empty result")
		}

		errorRow := result.Rows[0]
		if errorRow["error"] == nil {
			t.Fatalf("Expected error field in response, got: %+v", errorRow)
		}

		if errorRow["suggestion"] == nil {
			t.Fatalf("Expected suggestion field in response, got: %+v", errorRow)
		}

		if errorRow["original_sql"] == nil {
			t.Fatalf("Expected original_sql field in response, got: %+v", errorRow)
		}

		// Check that note indicates the error
		if !strings.Contains(result.Note, "query failed") {
			t.Fatalf("Expected note to indicate query failed, got: %s", result.Note)
		}
	})

	// Test table error handling
	t.Run("table_does_not_exist", func(t *testing.T) {
		_, result, err := srv.handleAsk(context.Background(), nil, askInput{
			Query: "show me tables",
		})

		if err != nil {
			t.Fatalf("Expected graceful error handling, got error: %v", err)
		}

		// Should return some kind of error response
		if len(result.Rows) == 0 {
			t.Fatalf("Expected some result rows, got empty")
		}
	})

	// Test syntax error handling
	t.Run("syntax_error", func(t *testing.T) {
		_, result, err := srv.handleAsk(context.Background(), nil, askInput{
			Query: "count all users",
		})

		if err != nil {
			t.Fatalf("Expected graceful error handling, got error: %v", err)
		}

		// Should return some kind of error response
		if len(result.Rows) == 0 {
			t.Fatalf("Expected some result rows, got empty")
		}
	})
}

func TestSchemaLoadingIntegration(t *testing.T) {
	t.Parallel()

	db := mustPool(t)
	defer db.Close()
	setupComprehensiveTestData(t, db)

	// Test schema loading
	schema, err := loadSchema(context.Background(), db)
	if err != nil {
		t.Fatalf("Failed to load schema: %v", err)
	}

	if schema == "" {
		t.Fatalf("Expected non-empty schema")
	}

	// Check that schema contains expected elements
	if !strings.Contains(schema, "TABLE") {
		t.Fatalf("Expected schema to contain TABLE definitions, got: %s", schema)
	}

	if !strings.Contains(schema, "FK") {
		t.Fatalf("Expected schema to contain FK (foreign key) definitions, got: %s", schema)
	}

	// Test schema caching
	cache := &SchemaCache{}

	// First call should load from DB
	schema1, err := cache.Get(context.Background(), db)
	if err != nil {
		t.Fatalf("Failed to get schema from cache: %v", err)
	}

	// Second call should use cache
	schema2, err := cache.Get(context.Background(), db)
	if err != nil {
		t.Fatalf("Failed to get schema from cache: %v", err)
	}

	if schema1 != schema2 {
		t.Fatalf("Expected cached schema to match, got different results")
	}

	// Test cache expiration
	cache.expiresAt = time.Now().Add(-1 * time.Hour) // force expiration
	schema3, err := cache.Get(context.Background(), db)
	if err != nil {
		t.Fatalf("Failed to refresh expired cache: %v", err)
	}

	if schema3 == "" {
		t.Fatalf("Expected refreshed schema to be non-empty")
	}
}
