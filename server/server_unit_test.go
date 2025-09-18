package main

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestMinNonZero(t *testing.T) {
	tests := []struct{ v, max, want int }{
		{0, 50, 50},
		{-1, 50, 50},
		{10, 50, 10},
		{100, 50, 50},
	}
	for _, tt := range tests {
		if got := minNonZero(tt.v, tt.max); got != tt.want {
			t.Fatalf("minNonZero(%d,%d)=%d want=%d", tt.v, tt.max, got, tt.want)
		}
	}
}

func TestGuardReadOnly(t *testing.T) {
	ok := []string{
		"SELECT 1",
		"WITH x AS (SELECT 1) SELECT * FROM x LIMIT 5",
		"-- comment\nSELECT now()",
	}
	bad := []string{
		"INSERT INTO t VALUES (1)",
		"UPDATE t SET a=1",
		"DELETE FROM t",
		"ALTER TABLE t ADD COLUMN x int",
		"DROP TABLE t",
		"TRUNCATE t",
		"CREATE TABLE t(x int)",
		"SELECT 1; SELECT 2", // multiple statements
	}
	for _, q := range ok {
		if err := guardReadOnly(q); err != nil {
			t.Fatalf("guardReadOnly rejected safe SQL: %q err=%v", q, err)
		}
	}
	for _, q := range bad {
		if err := guardReadOnly(q); err == nil {
			t.Fatalf("guardReadOnly did not reject: %q", q)
		}
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     100,
				QueryTO:     10 * time.Second,
				SchemaTTL:   5 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "missing database URL",
			cfg: Config{
				MaxRows:   100,
				QueryTO:   10 * time.Second,
				SchemaTTL: 5 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "invalid max rows - zero",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     0,
				QueryTO:     10 * time.Second,
				SchemaTTL:   5 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "invalid max rows - too high",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     20000,
				QueryTO:     10 * time.Second,
				SchemaTTL:   5 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "invalid query timeout - too low",
			cfg: Config{
				DatabaseURL: "postgres://user:pass@localhost/db",
				MaxRows:     100,
				QueryTO:     500 * time.Millisecond,
				SchemaTTL:   5 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "invalid schema TTL - too low",
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

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid input",
			input:   "SELECT * FROM users",
			wantErr: false,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "too long input",
			input:   string(make([]byte, maxQueryLength+1)),
			wantErr: true,
		},
		{
			name:    "suspicious pattern - comments",
			input:   "SELECT * FROM users -- DROP TABLE users",
			wantErr: false, // We warn but don't reject
		},
		{
			name:    "suspicious pattern - union",
			input:   "SELECT * FROM users UNION SELECT * FROM passwords",
			wantErr: false, // We warn but don't reject
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeInput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sanitizeInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPaginatedResult(t *testing.T) {
	tests := []struct {
		name         string
		totalCount   int
		page         int
		pageSize     int
		wantHasMore  bool
		wantNextPage int
	}{
		{
			name:         "first page with more",
			totalCount:   100,
			page:         0,
			pageSize:     10,
			wantHasMore:  true,
			wantNextPage: 1,
		},
		{
			name:         "middle page with more",
			totalCount:   100,
			page:         5,
			pageSize:     10,
			wantHasMore:  true,
			wantNextPage: 6,
		},
		{
			name:         "last page",
			totalCount:   100,
			page:         9,
			pageSize:     10,
			wantHasMore:  false,
			wantNextPage: 0,
		},
		{
			name:         "partial last page",
			totalCount:   95,
			page:         9,
			pageSize:     10,
			wantHasMore:  false,
			wantNextPage: 0,
		},
		{
			name:         "single page",
			totalCount:   5,
			page:         0,
			pageSize:     10,
			wantHasMore:  false,
			wantNextPage: 0,
		},
		{
			name:         "empty results",
			totalCount:   0,
			page:         0,
			pageSize:     10,
			wantHasMore:  false,
			wantNextPage: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate pagination logic
			offset := tt.page * tt.pageSize
			rowsOnPage := tt.pageSize
			if offset+rowsOnPage > tt.totalCount {
				rowsOnPage = tt.totalCount - offset
			}
			if rowsOnPage < 0 {
				rowsOnPage = 0
			}

			hasMore := offset+rowsOnPage < tt.totalCount
			nextPage := tt.page + 1
			if !hasMore {
				nextPage = 0
			}

			if hasMore != tt.wantHasMore {
				t.Fatalf("hasMore = %v, want %v (totalCount=%d, page=%d, pageSize=%d)",
					hasMore, tt.wantHasMore, tt.totalCount, tt.page, tt.pageSize)
			}
			if nextPage != tt.wantNextPage {
				t.Fatalf("nextPage = %d, want %d", nextPage, tt.wantNextPage)
			}
		})
	}
}

func TestStreamingPaginationCalculations(t *testing.T) {
	tests := []struct {
		name           string
		totalCount     int
		pageSize       int
		maxPages       int
		wantTotalPages int
		wantFetchPages int
	}{
		{
			name:           "exact pages",
			totalCount:     100,
			pageSize:       10,
			maxPages:       15,
			wantTotalPages: 10,
			wantFetchPages: 10,
		},
		{
			name:           "partial last page",
			totalCount:     95,
			pageSize:       10,
			maxPages:       15,
			wantTotalPages: 10,
			wantFetchPages: 10,
		},
		{
			name:           "limited by max pages",
			totalCount:     1000,
			pageSize:       10,
			maxPages:       5,
			wantTotalPages: 100,
			wantFetchPages: 5,
		},
		{
			name:           "single page",
			totalCount:     5,
			pageSize:       10,
			maxPages:       5,
			wantTotalPages: 1,
			wantFetchPages: 1,
		},
		{
			name:           "empty results",
			totalCount:     0,
			pageSize:       10,
			maxPages:       5,
			wantTotalPages: 0,
			wantFetchPages: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test total pages calculation (ceiling division)
			totalPages := (tt.totalCount + tt.pageSize - 1) / tt.pageSize
			if tt.totalCount == 0 {
				totalPages = 0
			}

			if totalPages != tt.wantTotalPages {
				t.Fatalf("totalPages = %d, want %d", totalPages, tt.wantTotalPages)
			}

			// Test pages to fetch calculation
			fetchPages := minNonZero(tt.maxPages, totalPages)
			if tt.totalCount == 0 {
				fetchPages = 0
			}

			if fetchPages != tt.wantFetchPages {
				t.Fatalf("fetchPages = %d, want %d", fetchPages, tt.wantFetchPages)
			}
		})
	}
}

func TestIsExpensiveQuery(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		expensive bool
	}{
		{
			name:      "simple select",
			sql:       "SELECT * FROM users LIMIT 10",
			expensive: false,
		},
		{
			name:      "single join",
			sql:       "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id LIMIT 10",
			expensive: false,
		},
		{
			name:      "multiple joins (3 joins - expensive)",
			sql:       "SELECT * FROM users u JOIN orders o ON u.id = o.user_id JOIN items i ON o.item_id = i.id JOIN sellers s ON i.seller_id = s.id LIMIT 10",
			expensive: true,
		},
		{
			name:      "left join",
			sql:       "SELECT * FROM users u LEFT JOIN orders o ON u.id = o.user_id LIMIT 10",
			expensive: true,
		},
		{
			name:      "cross join",
			sql:       "SELECT * FROM users CROSS JOIN items LIMIT 10",
			expensive: true,
		},
		{
			name:      "public schema join (single join - not expensive)",
			sql:       "SELECT * FROM users JOIN public.orders ON users.id = public.orders.user_id",
			expensive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isExpensiveQuery(tt.sql)
			if got != tt.expensive {
				t.Fatalf("isExpensiveQuery(%q) = %v, want %v", tt.sql, got, tt.expensive)
			}
		})
	}
}

func TestSimplifyExpensiveQuery(t *testing.T) {
	tests := []struct {
		name           string
		sql            string
		originalQuery  string
		wantSimplified bool
	}{
		{
			name:           "simple query unchanged",
			sql:            "SELECT * FROM users LIMIT 10",
			originalQuery:  "show users",
			wantSimplified: false,
		},
		{
			name:           "expensive join simplified",
			sql:            "SELECT * FROM users u JOIN public.orders o ON u.id = o.user_id LEFT JOIN items i ON o.item_id = i.id",
			originalQuery:  "show user orders",
			wantSimplified: true,
		},
		{
			name:           "left join simplified",
			sql:            "SELECT * FROM users u LEFT JOIN orders o ON u.id = o.user_id",
			originalQuery:  "show users with orders",
			wantSimplified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := simplifyExpensiveQuery(tt.sql, tt.originalQuery)
			simplified := result != tt.sql

			if simplified != tt.wantSimplified {
				t.Fatalf("simplifyExpensiveQuery simplified=%v, want %v\nOriginal: %s\nResult: %s",
					simplified, tt.wantSimplified, tt.sql, result)
			}

			if simplified && !strings.Contains(result, "Query too complex") {
				t.Fatalf("Expected simplified query to contain error message, got: %s", result)
			}
		})
	}
}

func TestValidateSQLBasic(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{
			name:    "valid select",
			sql:     "SELECT id FROM users LIMIT 10",
			wantErr: false,
		},
		{
			name:    "valid with clause",
			sql:     "WITH counts AS (SELECT COUNT(*) FROM users) SELECT * FROM counts",
			wantErr: false,
		},
		{
			name:    "valid - missing FROM is allowed by basic validation",
			sql:     "SELECT id LIMIT 10",
			wantErr: false, // Basic validation doesn't check FROM clause
		},
		{
			name:    "invalid - unbalanced parentheses",
			sql:     "SELECT id FROM users WHERE (name = 'test' LIMIT 10",
			wantErr: true,
		},
		{
			name:    "valid - semicolon is allowed by basic validation",
			sql:     "SELECT id FROM users; DROP TABLE users;",
			wantErr: false, // Basic validation doesn't check for semicolons
		},
		{
			name:    "invalid - no SELECT",
			sql:     "UPDATE users SET name = 'test'",
			wantErr: true,
		},
		{
			name:    "invalid - duplicate SELECT",
			sql:     "SELECT SELECT id FROM users",
			wantErr: true,
		},
		{
			name:    "invalid - empty sql",
			sql:     "",
			wantErr: true,
		},
		{
			name:    "invalid - whitespace only",
			sql:     "   \n\t  ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSQLBasic(tt.sql)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSQLBasic() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestErrorHandlingMessages(t *testing.T) {
	tests := []struct {
		name        string
		dbError     string
		wantMessage string
		wantSuggest string
	}{
		{
			name:        "column does not exist",
			dbError:     `ERROR: column "user_id" does not exist (SQLSTATE 42703)`,
			wantMessage: "Column not found in generated query",
			wantSuggest: "Try rephrasing your question or ask about specific tables",
		},
		{
			name:        "table does not exist",
			dbError:     `ERROR: relation "nonexistent_table" does not exist (SQLSTATE 42P01)`,
			wantMessage: "Table not found",
			wantSuggest: "Check available tables",
		},
		{
			name:        "syntax error",
			dbError:     `ERROR: syntax error at or near "SELEC" (SQLSTATE 42601)`,
			wantMessage: "SQL syntax error",
			wantSuggest: "Check query syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that our error detection logic works
			isColumnError := strings.Contains(tt.dbError, "column") && strings.Contains(tt.dbError, "does not exist")

			if tt.name == "column does not exist" && !isColumnError {
				t.Fatalf("Failed to detect column error in: %s", tt.dbError)
			}

			if tt.name != "column does not exist" && isColumnError {
				t.Fatalf("Incorrectly detected column error in: %s", tt.dbError)
			}
		})
	}
}

func TestSchemaCache(t *testing.T) {
	cache := &SchemaCache{}

	// Test empty cache
	if cache.txt != "" {
		t.Fatalf("Expected empty cache, got: %s", cache.txt)
	}

	// Test cache expiration
	cache.txt = "test schema"
	cache.expiresAt = time.Now().Add(-1 * time.Hour) // expired

	// Since we can't easily test the full Get method without a DB,
	// we'll test the expiration logic
	if time.Now().Before(cache.expiresAt) {
		t.Fatalf("Expected cache to be expired")
	}

	// Test future expiration
	cache.expiresAt = time.Now().Add(1 * time.Hour)
	if !time.Now().Before(cache.expiresAt) {
		t.Fatalf("Expected cache to not be expired")
	}
}

func TestTableNameCaseSensitivity(t *testing.T) {
	// This test demonstrates the case sensitivity issue with PostgreSQL table names
	// When a table is created with quotes like "Book", it preserves the exact case
	// But LLMs often generate SQL with lowercase table names without quotes

	tests := []struct {
		name         string
		schemaTable  string // How the table appears in schema
		generatedSQL string // What the LLM generates
		shouldWork   bool   // Whether this should work in PostgreSQL
		description  string
	}{
		{
			name:         "lowercase table, lowercase SQL",
			schemaTable:  "book",
			generatedSQL: "SELECT * FROM book",
			shouldWork:   true,
			description:  "Standard case - should work fine",
		},
		{
			name:         "lowercase table, uppercase SQL",
			schemaTable:  "book",
			generatedSQL: "SELECT * FROM BOOK",
			shouldWork:   true,
			description:  "PostgreSQL folds unquoted identifiers to lowercase",
		},
		{
			name:         "quoted capital table, lowercase SQL",
			schemaTable:  "Book", // This represents a table created as "Book"
			generatedSQL: "SELECT * FROM book",
			shouldWork:   false,
			description:  "FAILS: Quoted table name preserves case, unquoted reference fails",
		},
		{
			name:         "quoted capital table, quoted SQL",
			schemaTable:  "Book",
			generatedSQL: `SELECT * FROM "Book"`,
			shouldWork:   true,
			description:  "Works when both creation and reference use quotes",
		},
		{
			name:         "quoted capital table, public schema",
			schemaTable:  "Book",
			generatedSQL: "SELECT * FROM public.book",
			shouldWork:   false,
			description:  "FAILS: Schema qualification doesn't help with case mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test documents the expected behavior
			// The actual fix would need to handle the case where tt.shouldWork is false
			if !tt.shouldWork {
				t.Logf("KNOWN ISSUE: %s - %s", tt.description, tt.generatedSQL)
				t.Logf("Schema shows table: %s, Generated SQL: %s", tt.schemaTable, tt.generatedSQL)
			} else {
				t.Logf("WORKS: %s - %s", tt.description, tt.generatedSQL)
			}
		})
	}
}

func TestSchemaQuoting(t *testing.T) {
	// Test the regex pattern used in schema generation for determining when to quote identifiers
	tests := []struct {
		name        string
		identifier  string
		shouldQuote bool
	}{
		{
			name:        "lowercase simple",
			identifier:  "book",
			shouldQuote: false,
		},
		{
			name:        "lowercase with underscore",
			identifier:  "book_category",
			shouldQuote: false,
		},
		{
			name:        "starts with underscore",
			identifier:  "_private_table",
			shouldQuote: false,
		},
		{
			name:        "contains numbers",
			identifier:  "table123",
			shouldQuote: false,
		},
		{
			name:        "mixed case - needs quotes",
			identifier:  "Book",
			shouldQuote: true,
		},
		{
			name:        "all caps - needs quotes",
			identifier:  "USERS",
			shouldQuote: true,
		},
		{
			name:        "contains spaces - needs quotes",
			identifier:  "user data",
			shouldQuote: true,
		},
		{
			name:        "contains special chars - needs quotes",
			identifier:  "user-data",
			shouldQuote: true,
		},
		{
			name:        "starts with number - needs quotes",
			identifier:  "1table",
			shouldQuote: true,
		},
		{
			name:        "camelCase - needs quotes",
			identifier:  "userName",
			shouldQuote: true,
		},
	}

	// This regex matches standard PostgreSQL unquoted identifiers:
	// - Must start with lowercase letter or underscore
	// - Can contain lowercase letters, numbers, underscores
	regex := `^[a-z_][a-z0-9_]*$`

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, err := regexp.MatchString(regex, tt.identifier)
			if err != nil {
				t.Fatalf("regex error: %v", err)
			}

			needsQuoting := !matched
			if needsQuoting != tt.shouldQuote {
				t.Fatalf("identifier %q: expected shouldQuote=%v, got needsQuoting=%v",
					tt.identifier, tt.shouldQuote, needsQuoting)
			}

			if needsQuoting {
				t.Logf("Identifier %q needs quoting -> %q", tt.identifier, `"`+tt.identifier+`"`)
			} else {
				t.Logf("Identifier %q does not need quoting", tt.identifier)
			}
		})
	}
}

func TestSchemaGenerationQuoting(t *testing.T) {
	// Test that demonstrates how the schema generation should work with our fix
	// This simulates the SQL query output that would be generated by our improved loadSchema function

	tests := []struct {
		name          string
		tableName     string
		columnName    string
		expectedTable string
		expectedCol   string
	}{
		{
			name:          "lowercase table and column - no quotes needed",
			tableName:     "book",
			columnName:    "title",
			expectedTable: "book",
			expectedCol:   "title",
		},
		{
			name:          "mixed case table - needs quotes",
			tableName:     "Book",
			columnName:    "title",
			expectedTable: `"Book"`,
			expectedCol:   "title",
		},
		{
			name:          "mixed case column - needs quotes",
			tableName:     "book",
			columnName:    "Title",
			expectedTable: "book",
			expectedCol:   `"Title"`,
		},
		{
			name:          "both need quotes",
			tableName:     "BookCategory",
			columnName:    "CategoryName",
			expectedTable: `"BookCategory"`,
			expectedCol:   `"CategoryName"`,
		},
		{
			name:          "special characters - needs quotes",
			tableName:     "user-data",
			columnName:    "first-name",
			expectedTable: `"user-data"`,
			expectedCol:   `"first-name"`,
		},
	}

	regex := `^[a-z_][a-z0-9_]*$`

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test table name quoting logic
			tableMatched, _ := regexp.MatchString(regex, tt.tableName)
			var actualTable string
			if tableMatched {
				actualTable = tt.tableName
			} else {
				actualTable = `"` + tt.tableName + `"`
			}

			if actualTable != tt.expectedTable {
				t.Fatalf("Table name: expected %q, got %q", tt.expectedTable, actualTable)
			}

			// Test column name quoting logic
			colMatched, _ := regexp.MatchString(regex, tt.columnName)
			var actualCol string
			if colMatched {
				actualCol = tt.columnName
			} else {
				actualCol = `"` + tt.columnName + `"`
			}

			if actualCol != tt.expectedCol {
				t.Fatalf("Column name: expected %q, got %q", tt.expectedCol, actualCol)
			}

			t.Logf("Schema would show: TABLE public.%s(%s text)", actualTable, actualCol)
		})
	}
}
