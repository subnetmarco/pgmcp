package main

import (
	"strings"
	"testing"
	"time"
)

func TestSanitizeInputComprehensive(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		wantWarn bool // Should trigger warning log
	}{
		// Valid inputs
		{
			name:    "normal_query",
			input:   "SELECT * FROM users WHERE id = 1",
			wantErr: false,
		},
		{
			name:    "natural_language",
			input:   "Show me all customers from New York",
			wantErr: false,
		},
		{
			name:    "with_numbers",
			input:   "Find orders over $100",
			wantErr: false,
		},

		// Invalid inputs
		{
			name:    "empty_string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "only_whitespace",
			input:   "   \t\n  ",
			wantErr: true,
		},
		{
			name:    "too_long",
			input:   strings.Repeat("SELECT * FROM table ", 1000), // > maxQueryLength
			wantErr: true,
		},

		// Suspicious patterns (warn but don't reject)
		{
			name:     "sql_comment",
			input:    "SELECT * FROM users -- DROP TABLE users",
			wantErr:  false,
			wantWarn: true,
		},
		{
			name:     "block_comment",
			input:    "SELECT * FROM users /* comment */",
			wantErr:  false,
			wantWarn: true,
		},
		{
			name:     "union_query",
			input:    "SELECT * FROM users UNION SELECT * FROM admins",
			wantErr:  false,
			wantWarn: true,
		},
		{
			name:     "system_procedure",
			input:    "EXEC xp_cmdshell 'dir'",
			wantErr:  false,
			wantWarn: true,
		},
		{
			name:     "information_schema",
			input:    "SELECT * FROM information_schema.tables",
			wantErr:  false,
			wantWarn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeInput(tt.input)
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Fatalf("sanitizeInput(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}

			// Note: We can't easily test warning logs in unit tests without capturing log output
			// In a real implementation, you might use a test logger
		})
	}
}

func TestGuardReadOnlyComprehensive(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		// Safe queries
		{
			name:    "simple_select",
			sql:     "SELECT * FROM users",
			wantErr: false,
		},
		{
			name:    "with_clause",
			sql:     "WITH top_users AS (SELECT * FROM users) SELECT * FROM top_users",
			wantErr: false,
		},
		{
			name:    "complex_select",
			sql:     "SELECT u.name, COUNT(o.id) FROM users u LEFT JOIN orders o ON u.id = o.user_id GROUP BY u.id, u.name",
			wantErr: false,
		},
		{
			name:    "select_with_comment",
			sql:     "-- This is a comment\nSELECT * FROM users",
			wantErr: false,
		},

		// Dangerous queries
		{
			name:    "insert",
			sql:     "INSERT INTO users (name) VALUES ('hacker')",
			wantErr: true,
		},
		{
			name:    "update",
			sql:     "UPDATE users SET name = 'changed' WHERE id = 1",
			wantErr: true,
		},
		{
			name:    "delete",
			sql:     "DELETE FROM users WHERE id = 1",
			wantErr: true,
		},
		{
			name:    "drop_table",
			sql:     "DROP TABLE users",
			wantErr: true,
		},
		{
			name:    "alter_table",
			sql:     "ALTER TABLE users ADD COLUMN hacked TEXT",
			wantErr: true,
		},
		{
			name:    "truncate",
			sql:     "TRUNCATE TABLE users",
			wantErr: true,
		},
		{
			name:    "create_table",
			sql:     "CREATE TABLE malicious (id INT)",
			wantErr: true,
		},
		{
			name:    "grant_permissions",
			sql:     "GRANT ALL ON users TO public",
			wantErr: true,
		},
		{
			name:    "revoke_permissions",
			sql:     "REVOKE SELECT ON users FROM public",
			wantErr: true,
		},
		{
			name:    "case_insensitive_insert",
			sql:     "insert into users (name) values ('test')",
			wantErr: true,
		},
		{
			name:    "mixed_case_delete",
			sql:     "Delete From users Where id = 1",
			wantErr: true,
		},

		// Multiple statements
		{
			name:    "multiple_statements",
			sql:     "SELECT * FROM users; SELECT * FROM orders",
			wantErr: true,
		},
		{
			name:    "select_with_semicolon_end",
			sql:     "SELECT * FROM users;",
			wantErr: false, // Single statement with trailing semicolon is OK
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := guardReadOnly(tt.sql)
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Fatalf("guardReadOnly(%q) error = %v, wantErr %v", tt.sql, err, tt.wantErr)
			}
		})
	}
}

func TestMinNonZeroEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		v    int
		max  int
		want int
	}{
		{"zero_zero", 0, 0, 0},
		{"negative_zero", -1, 0, 0},
		{"positive_zero", 5, 0, 0},
		{"zero_positive", 0, 10, 10},
		{"equal_values", 5, 5, 5},
		{"large_numbers", 1000000, 999999, 999999},
		{"max_int", 2147483647, 2147483646, 2147483646},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := minNonZero(tt.v, tt.max)
			if got != tt.want {
				t.Fatalf("minNonZero(%d, %d) = %d, want %d", tt.v, tt.max, got, tt.want)
			}
		})
	}
}

func TestConfigValidationEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "boundary_values_valid",
			cfg: Config{
				DatabaseURL: "postgres://test",
				MaxRows:     1,
				QueryTO:     1 * time.Second,
				SchemaTTL:   30 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "boundary_values_max_valid",
			cfg: Config{
				DatabaseURL: "postgres://test",
				MaxRows:     10000,
				QueryTO:     5 * time.Minute,
				SchemaTTL:   24 * time.Hour,
			},
			wantErr: false,
		},
		{
			name: "max_rows_boundary_exceeded",
			cfg: Config{
				DatabaseURL: "postgres://test",
				MaxRows:     10001,
				QueryTO:     10 * time.Second,
				SchemaTTL:   5 * time.Minute,
			},
			wantErr: true,
			errMsg:  "cannot exceed 10000",
		},
		{
			name: "query_timeout_too_short",
			cfg: Config{
				DatabaseURL: "postgres://test",
				MaxRows:     100,
				QueryTO:     999 * time.Millisecond,
				SchemaTTL:   5 * time.Minute,
			},
			wantErr: true,
			errMsg:  "at least 1 second",
		},
		{
			name: "schema_ttl_too_short",
			cfg: Config{
				DatabaseURL: "postgres://test",
				MaxRows:     100,
				QueryTO:     10 * time.Second,
				SchemaTTL:   29 * time.Second,
			},
			wantErr: true,
			errMsg:  "at least 30 seconds",
		},
		{
			name: "multiple_validation_errors",
			cfg: Config{
				DatabaseURL: "", // Missing
				MaxRows:     0,  // Invalid
				QueryTO:     0,  // Invalid
				SchemaTTL:   0,  // Invalid
			},
			wantErr: true,
			errMsg:  "configuration validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			hasErr := err != nil

			if hasErr != tt.wantErr {
				t.Fatalf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error to contain %q, got %v", tt.errMsg, err)
				}
			}
		})
	}
}
