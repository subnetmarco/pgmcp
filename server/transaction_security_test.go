package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestTransactionEscapeAttempts(t *testing.T) {
	// Test attempts to escape read-only transaction scope
	// Addresses GitHub issue: https://github.com/subnetmarco/pgmcp/issues/7

	escapeAttempts := []struct {
		name        string
		sql         string
		shouldBlock bool
		description string
	}{
		{
			name:        "rollback_and_drop",
			sql:         "ROLLBACK; DROP TABLE users;",
			shouldBlock: true,
			description: "Attempt to rollback transaction and drop table",
		},
		{
			name:        "commit_and_insert",
			sql:         "COMMIT; INSERT INTO users VALUES (999, 'hacker');",
			shouldBlock: true,
			description: "Attempt to commit transaction and insert data",
		},
		{
			name:        "multiple_statements_with_ddl",
			sql:         "SELECT 1; CREATE TABLE evil (id INT); DROP TABLE users;",
			shouldBlock: true,
			description: "Multiple statements with DDL operations",
		},
		{
			name:        "begin_new_transaction",
			sql:         "BEGIN; UPDATE users SET email = 'hacked'; COMMIT;",
			shouldBlock: true,
			description: "Attempt to begin new transaction with updates",
		},
		{
			name:        "savepoint_manipulation",
			sql:         "SAVEPOINT sp1; DELETE FROM users; RELEASE SAVEPOINT sp1;",
			shouldBlock: true,
			description: "Attempt to use savepoints for data manipulation",
		},
		{
			name:        "set_transaction_readwrite",
			sql:         "SET TRANSACTION READ WRITE; UPDATE users SET name = 'hacked';",
			shouldBlock: true,
			description: "Attempt to change transaction mode to read-write",
		},
		{
			name:        "function_with_side_effects",
			sql:         "SELECT pg_cancel_backend(pg_backend_pid());",
			shouldBlock: false, // This is a SELECT, should pass our guards but fail at DB level
			description: "System function with side effects",
		},
		{
			name:        "copy_statement",
			sql:         "COPY users FROM '/tmp/evil.csv';",
			shouldBlock: true,
			description: "Attempt to use COPY statement for data import",
		},
		{
			name:        "truncate_attempt",
			sql:         "TRUNCATE TABLE users;",
			shouldBlock: true,
			description: "Attempt to truncate table",
		},
		{
			name:        "alter_table_attempt",
			sql:         "ALTER TABLE users ADD COLUMN hacked TEXT;",
			shouldBlock: true,
			description: "Attempt to alter table structure",
		},
		{
			name:        "vacuum_attempt",
			sql:         "VACUUM FULL users;",
			shouldBlock: true,
			description: "Attempt to run maintenance commands",
		},
		{
			name:        "grant_permissions",
			sql:         "GRANT ALL ON users TO public;",
			shouldBlock: true,
			description: "Attempt to grant permissions",
		},
	}

	for _, tt := range escapeAttempts {
		t.Run(tt.name, func(t *testing.T) {
			err := guardReadOnly(tt.sql)

			if tt.shouldBlock {
				if err == nil {
					t.Fatalf("Expected %s to be blocked, but it was allowed: %s", tt.description, tt.sql)
				}
				t.Logf("Correctly blocked: %s", tt.description)
			} else {
				if err != nil {
					t.Fatalf("Expected %s to be allowed, but it was blocked: %v", tt.description, err)
				}
				t.Logf("Correctly allowed: %s", tt.description)
			}
		})
	}
}

func TestDatabaseTransactionIsolation(t *testing.T) {
	// Test that even if SQL injection bypasses our guards,
	// the PostgreSQL read-only transaction prevents any modifications
	// This tests the database-level protection as mentioned in the GitHub issue

	db := mustPool(t)
	defer db.Close()
	resetSchema(t, db)

	// Test various write operations that should be rejected by PostgreSQL
	writeAttempts := []struct {
		name string
		sql  string
	}{
		{"insert", "INSERT INTO users (email, first_name, last_name) VALUES ('test@test.com', 'Test', 'User')"},
		{"update", "UPDATE users SET first_name = 'Hacked' WHERE id = 1"},
		{"delete", "DELETE FROM users WHERE id = 1"},
		{"drop_table", "DROP TABLE users"},
		{"create_table", "CREATE TABLE evil (id INT)"},
		{"truncate", "TRUNCATE TABLE users"},
		{"alter_table", "ALTER TABLE users ADD COLUMN evil TEXT"},
	}

	ctx := context.Background()
	for _, attempt := range writeAttempts {
		t.Run(attempt.name, func(t *testing.T) {
			// Create a new connection and transaction for each test
			conn, err := db.Acquire(ctx)
			if err != nil {
				t.Fatalf("Failed to acquire connection: %v", err)
			}
			defer conn.Release()

			tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatalf("Failed to begin read-only transaction: %v", err)
			}
			defer tx.Rollback(ctx)

			_, err = tx.Exec(ctx, attempt.sql)
			if err == nil {
				t.Fatalf("Expected PostgreSQL to reject %s in read-only transaction, but it was allowed", attempt.name)
			}

			// Verify it's specifically a read-only transaction error
			if !strings.Contains(err.Error(), "read-only") && !strings.Contains(err.Error(), "cannot execute") {
				t.Fatalf("Expected read-only transaction error, got: %v", err)
			}

			t.Logf("PostgreSQL correctly rejected %s: %v", attempt.name, err)
		})
	}
}

func TestMultiStatementInjectionPrevention(t *testing.T) {
	// Test prevention of multi-statement SQL injection attempts
	// Even if multiple statements slip through, each should be checked

	multiStatementAttempts := []string{
		"SELECT 1; DROP TABLE users;",
		"SELECT * FROM users; INSERT INTO users VALUES (999, 'evil');",
		"SELECT COUNT(*) FROM users; TRUNCATE users;",
		"/* comment */ SELECT 1; DELETE FROM users;",
		"SELECT 1 --comment\n; UPDATE users SET name = 'hacked';",
	}

	for i, sql := range multiStatementAttempts {
		t.Run(fmt.Sprintf("multi_statement_%d", i), func(t *testing.T) {
			err := guardReadOnly(sql)
			if err == nil {
				t.Fatalf("Expected multi-statement SQL to be blocked: %s", sql)
			}
			t.Logf("Correctly blocked multi-statement injection: %s", sql)
		})
	}
}

func TestAdvancedSQLInjectionAttempts(t *testing.T) {
	// Test sophisticated SQL injection attempts that try to bypass security

	injectionAttempts := []struct {
		name        string
		sql         string
		description string
	}{
		{
			name:        "union_based_injection",
			sql:         "SELECT id FROM users UNION SELECT 1; DROP TABLE users; --",
			description: "UNION-based injection with DDL",
		},
		{
			name:        "comment_bypass",
			sql:         "SELECT 1 /* comment */; INSERT INTO users VALUES (1); -- comment",
			description: "Comment-based statement separation",
		},
		{
			name:        "nested_transaction_control",
			sql:         "SELECT (SELECT 1 FROM (SELECT 1) x); ROLLBACK; CREATE TABLE evil;",
			description: "Nested subqueries with transaction control",
		},
		{
			name:        "function_call_with_writes",
			sql:         "SELECT pg_advisory_lock(1); UPDATE users SET name = 'locked';",
			description: "Advisory lock attempt with update",
		},
		{
			name:        "cte_with_modification",
			sql:         "WITH evil AS (UPDATE users SET name = 'hacked' RETURNING *) SELECT * FROM evil;",
			description: "CTE with data modification",
		},
	}

	for _, attempt := range injectionAttempts {
		t.Run(attempt.name, func(t *testing.T) {
			err := guardReadOnly(attempt.sql)
			if err == nil {
				t.Fatalf("Expected sophisticated injection to be blocked: %s - %s", attempt.description, attempt.sql)
			}
			t.Logf("Correctly blocked %s: %s", attempt.description, attempt.sql)
		})
	}
}
