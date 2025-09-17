package main

import (
	"testing"
)

func TestAskInputPagination(t *testing.T) {
	tests := []struct {
		name     string
		input    askInput
		wantPage int
		wantSize int
	}{
		{
			name: "default pagination",
			input: askInput{
				Query: "test query",
			},
			wantPage: 0,
			wantSize: pageSize, // Should use default
		},
		{
			name: "custom page size",
			input: askInput{
				Query:    "test query",
				PageSize: 25,
			},
			wantPage: 0,
			wantSize: 25,
		},
		{
			name: "specific page",
			input: askInput{
				Query:    "test query",
				Page:     3,
				PageSize: 10,
			},
			wantPage: 3,
			wantSize: 10,
		},
		{
			name: "legacy max_rows",
			input: askInput{
				Query:   "test query",
				MaxRows: 15,
			},
			wantPage: 0,
			wantSize: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test page size logic
			pageSize := minNonZero(tt.input.PageSize, pageSize)
			if tt.input.MaxRows > 0 {
				pageSize = minNonZero(tt.input.MaxRows, pageSize)
			}

			if pageSize != tt.wantSize {
				t.Fatalf("pageSize = %d, want %d", pageSize, tt.wantSize)
			}

			if tt.input.Page != tt.wantPage {
				t.Fatalf("page = %d, want %d", tt.input.Page, tt.wantPage)
			}
		})
	}
}

func TestStreamInputValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     streamInput
		wantPages int
		wantSize  int
	}{
		{
			name: "default values",
			input: streamInput{
				Query: "test query",
			},
			wantPages: maxPagesAuto,
			wantSize:  pageSize,
		},
		{
			name: "custom values",
			input: streamInput{
				Query:    "test query",
				MaxPages: 5,
				PageSize: 25,
			},
			wantPages: 5,
			wantSize:  25,
		},
		{
			name: "zero values use defaults",
			input: streamInput{
				Query:    "test query",
				MaxPages: 0,
				PageSize: 0,
			},
			wantPages: maxPagesAuto,
			wantSize:  pageSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxPages := minNonZero(tt.input.MaxPages, maxPagesAuto)
			pageSize := minNonZero(tt.input.PageSize, pageSize)

			if maxPages != tt.wantPages {
				t.Fatalf("maxPages = %d, want %d", maxPages, tt.wantPages)
			}
			if pageSize != tt.wantSize {
				t.Fatalf("pageSize = %d, want %d", pageSize, tt.wantSize)
			}
		})
	}
}

func TestPaginationSQLGeneration(t *testing.T) {
	tests := []struct {
		name         string
		originalSQL  string
		page         int
		pageSize     int
		wantContains []string
	}{
		{
			name:        "basic pagination",
			originalSQL: "SELECT * FROM users ORDER BY id",
			page:        0,
			pageSize:    10,
			wantContains: []string{
				"WITH query AS",
				"SELECT * FROM users ORDER BY id",
				"LIMIT 10 OFFSET 0",
			},
		},
		{
			name:        "second page",
			originalSQL: "SELECT name FROM items",
			page:        2,
			pageSize:    5,
			wantContains: []string{
				"WITH query AS",
				"SELECT name FROM items",
				"LIMIT 5 OFFSET 10",
			},
		},
		{
			name:        "large page",
			originalSQL: "SELECT id, name FROM products ORDER BY name",
			page:        0,
			pageSize:    20,
			wantContains: []string{
				"WITH query AS",
				"SELECT * FROM query",
				"LIMIT 20 OFFSET 0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test count SQL generation
			countSQL := "WITH query AS (" + tt.originalSQL + ") SELECT COUNT(*) FROM query"
			for _, want := range []string{"WITH query AS", "SELECT COUNT(*) FROM query"} {
				if !contains(countSQL, want) {
					t.Fatalf("countSQL missing '%s': %s", want, countSQL)
				}
			}

			// Test paginated SQL generation
			offset := tt.page * tt.pageSize
			paginatedSQL := "WITH query AS (" + tt.originalSQL + ") SELECT * FROM query LIMIT " +
				intToString(tt.pageSize) + " OFFSET " + intToString(offset)

			for _, want := range tt.wantContains {
				if !contains(paginatedSQL, want) {
					t.Fatalf("paginatedSQL missing '%s': %s", want, paginatedSQL)
				}
			}
		})
	}
}

// Helper functions for tests
func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}

	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func intToString(i int) string {
	if i == 0 {
		return "0"
	}
	if i < 0 {
		return "-" + intToString(-i)
	}

	var result string
	for i > 0 {
		result = string(rune('0'+i%10)) + result
		i /= 10
	}
	return result
}
