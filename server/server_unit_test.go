package main

import (
	"testing"
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
