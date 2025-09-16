package main

import (
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
