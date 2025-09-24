package main

import (
	"testing"
)

func TestTransactionControlBlocking(t *testing.T) {
	// Test that all transaction control commands are blocked
	// Addresses the specific concern about ROLLBACK and other commands

	transactionControlCommands := []struct {
		name string
		sql  string
	}{
		{"rollback", "ROLLBACK;"},
		{"commit", "COMMIT;"},
		{"begin", "BEGIN;"},
		{"start_transaction", "START TRANSACTION;"},
		{"savepoint", "SAVEPOINT sp1;"},
		{"release_savepoint", "RELEASE SAVEPOINT sp1;"},
		{"set_transaction", "SET TRANSACTION READ WRITE;"},
		{"set_isolation", "SET TRANSACTION ISOLATION LEVEL READ COMMITTED;"},
		{"rollback_to_savepoint", "ROLLBACK TO SAVEPOINT sp1;"},

		// Mixed case variants
		{"rollback_mixed", "RoLlBaCk;"},
		{"commit_mixed", "CoMmIt;"},
		{"begin_mixed", "BeGiN;"},

		// With additional statements
		{"rollback_with_ddl", "ROLLBACK; DROP TABLE users;"},
		{"commit_with_dml", "COMMIT; INSERT INTO users VALUES (1);"},
		{"set_with_update", "SET TRANSACTION READ WRITE; UPDATE users SET name = 'test';"},
	}

	for _, tc := range transactionControlCommands {
		t.Run(tc.name, func(t *testing.T) {
			err := guardReadOnly(tc.sql)
			if err == nil {
				t.Fatalf("Expected %s to be blocked, but it was allowed: %s", tc.name, tc.sql)
			}
			t.Logf("Correctly blocked %s: %s", tc.name, tc.sql)
		})
	}
}

func TestReadOnlyCommandsStillAllowed(t *testing.T) {
	// Ensure that legitimate read-only commands are still allowed

	allowedCommands := []struct {
		name string
		sql  string
	}{
		{"simple_select", "SELECT * FROM users;"},
		{"select_with_joins", "SELECT u.name, o.id FROM users u JOIN orders o ON u.id = o.user_id;"},
		{"select_with_cte", "WITH recent_users AS (SELECT * FROM users WHERE created_at > now() - interval '1 day') SELECT * FROM recent_users;"},
		{"select_with_functions", "SELECT COUNT(*), AVG(price_cents) FROM items;"},
		{"select_with_window", "SELECT name, ROW_NUMBER() OVER (ORDER BY created_at) FROM users;"},
		{"explain_query", "EXPLAIN SELECT * FROM users;"},
		{"show_tables", "SELECT tablename FROM pg_tables WHERE schemaname = 'public';"},
	}

	for _, tc := range allowedCommands {
		t.Run(tc.name, func(t *testing.T) {
			err := guardReadOnly(tc.sql)
			if err != nil {
				t.Fatalf("Expected %s to be allowed, but it was blocked: %v", tc.name, err)
			}
			t.Logf("Correctly allowed %s: %s", tc.name, tc.sql)
		})
	}
}
