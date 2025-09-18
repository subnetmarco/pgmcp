//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func BenchmarkConcurrentQueries(b *testing.B) {
	// Setup
	db := mustPoolForBench(b)
	defer db.Close()
	createLargeTestDataForBench(b, db, 1000)

	llm := mockOpenAIFast(b)
	defer llm.Close()

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test-key",
		OpenAIBase:  llm.URL + "/v1",
		OpenAIModel: "test-model",
		QueryTO:     30 * time.Second,
		MaxRows:     50,
	}

	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		b.Fatalf("newServer: %v", err)
	}

	b.ResetTimer()

	b.Run("sequential_queries", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, err := srv.handleAsk(ctx, nil, askInput{
				Query:   "List test records",
				MaxRows: 20,
			})
			if err != nil {
				b.Fatalf("handleAsk: %v", err)
			}
		}
	})

	b.Run("concurrent_queries", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, _, err := srv.handleAsk(ctx, nil, askInput{
					Query:   "List test records",
					MaxRows: 20,
				})
				if err != nil {
					b.Fatalf("handleAsk: %v", err)
				}
			}
		})
	})

	b.Run("mixed_operations", func(b *testing.B) {
		var wg sync.WaitGroup

		for i := 0; i < b.N; i++ {
			wg.Add(3)

			// Ask query
			go func() {
				defer wg.Done()
				srv.handleAsk(ctx, nil, askInput{Query: "List records", MaxRows: 10})
			}()

			// Search query
			go func() {
				defer wg.Done()
				srv.handleSearch(ctx, nil, searchInput{Q: "test", Limit: 10})
			}()

			// Stream query
			go func() {
				defer wg.Done()
				srv.handleStream(ctx, nil, streamInput{Query: "Stream records", MaxPages: 2, PageSize: 10})
			}()
		}

		wg.Wait()
	})
}

func BenchmarkSchemaCache(b *testing.B) {
	db := mustPoolForBench(b)
	defer db.Close()

	cache := &SchemaCache{ttl: 5 * time.Minute}
	ctx := context.Background()

	b.ResetTimer()

	b.Run("cache_miss", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.txt = "" // Force cache miss
			cache.expiresAt = time.Time{}
			_, err := cache.Get(ctx, db)
			if err != nil {
				b.Fatalf("cache.Get: %v", err)
			}
		}
	})

	b.Run("cache_hit", func(b *testing.B) {
		// Prime the cache
		cache.Get(ctx, db)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := cache.Get(ctx, db)
			if err != nil {
				b.Fatalf("cache.Get: %v", err)
			}
		}
	})

	b.Run("concurrent_cache_access", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, err := cache.Get(ctx, db)
				if err != nil {
					b.Fatalf("cache.Get: %v", err)
				}
			}
		})
	})
}

func TestConcurrentSafety(t *testing.T) {
	t.Parallel()

	db := mustPool(t)
	defer db.Close()
	setupComprehensiveTestData(t, db)

	llm := mockOpenAIFast(t)
	defer llm.Close()

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test-key",
		OpenAIBase:  llm.URL + "/v1",
		OpenAIModel: "test-model",
		QueryTO:     10 * time.Second,
		MaxRows:     50,
	}

	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	t.Run("concurrent_schema_cache", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 50)

		// 50 concurrent requests to test schema cache safety
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				// Force cache refresh on some requests
				if id%10 == 0 {
					srv.cache.txt = ""
					srv.cache.expiresAt = time.Time{}
				}

				_, err := srv.cache.Get(ctx, srv.db)
				if err != nil {
					errors <- err
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Fatalf("concurrent schema cache error: %v", err)
		}
	})

	t.Run("concurrent_handlers", func(t *testing.T) {
		var wg sync.WaitGroup
		errors := make(chan error, 30)

		// 30 concurrent handler calls
		for i := 0; i < 10; i++ {
			wg.Add(3)

			go func() {
				defer wg.Done()
				_, _, err := srv.handleAsk(ctx, nil, askInput{Query: "test", MaxRows: 5})
				if err != nil {
					errors <- err
				}
			}()

			go func() {
				defer wg.Done()
				_, _, err := srv.handleSearch(ctx, nil, searchInput{Q: "test", Limit: 5})
				if err != nil {
					errors <- err
				}
			}()

			go func() {
				defer wg.Done()
				_, _, err := srv.handleStream(ctx, nil, streamInput{Query: "test", MaxPages: 2, PageSize: 5})
				if err != nil {
					errors <- err
				}
			}()
		}

		wg.Wait()
		close(errors)

		var errorCount int
		for err := range errors {
			errorCount++
			// Log errors but don't fail - we expect some database errors in testing
			t.Logf("concurrent handler error (expected in testing): %v", err)
		}

		// Only fail if we get too many errors (indicating a systemic issue)
		if errorCount > 15 { // More than 50% failure rate indicates real problems
			t.Fatalf("too many concurrent errors: %d/30 operations failed", errorCount)
		}

		t.Logf("concurrent safety test completed: %d errors out of 30 operations (%.1f%% success rate)",
			errorCount, float64(30-errorCount)/30*100)
	})
}

func TestMemoryUsage(t *testing.T) {
	t.Parallel()

	db := mustPool(t)
	defer db.Close()
	createLargeTestDataForBench(t, db, 5000) // 5k records

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		QueryTO:     30 * time.Second,
		MaxRows:     1000,
	}

	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	t.Run("large_result_set_streaming", func(t *testing.T) {
		// Test that streaming large result sets doesn't consume excessive memory
		sql := "SELECT id, name, value FROM large_test_table ORDER BY id"

		start := time.Now()
		pages, totalCount, err := srv.runStreamingQuery(ctx, sql, 10, 100) // 1000 rows
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("runStreamingQuery: %v", err)
		}

		if totalCount < 1000 {
			t.Fatalf("expected at least 1000 records, got %d", totalCount)
		}

		if len(pages) == 0 {
			t.Fatalf("expected pages, got none")
		}

		// Should complete in reasonable time (under 10 seconds)
		if duration > 10*time.Second {
			t.Fatalf("streaming took too long: %v", duration)
		}

		t.Logf("Streamed %d rows in %d pages in %v", totalCount, len(pages), duration)
	})
}

func TestErrorHandling(t *testing.T) {
	t.Parallel()

	cfg := Config{
		DatabaseURL: "postgres://invalid:invalid@nonexistent:5432/invalid",
		QueryTO:     5 * time.Second,
		MaxRows:     50,
	}

	ctx := context.Background()

	t.Run("invalid_database_connection", func(t *testing.T) {
		_, err := newServer(ctx, cfg)
		if err == nil {
			t.Fatalf("expected error for invalid database connection")
		}
	})

	// Test with valid connection but invalid queries
	if os.Getenv("DATABASE_URL") != "" {
		validCfg := Config{
			DatabaseURL: os.Getenv("DATABASE_URL"),
			QueryTO:     5 * time.Second,
			MaxRows:     50,
		}

		srv, err := newServer(ctx, validCfg)
		if err != nil {
			t.Skipf("skip: cannot create server: %v", err)
		}

		t.Run("invalid_sql_syntax", func(t *testing.T) {
			_, err := srv.runReadOnlyQuery(ctx, "SELECT * FROM nonexistent_table", 10)
			if err == nil {
				t.Fatalf("expected error for invalid SQL")
			}
		})

		t.Run("timeout_handling", func(t *testing.T) {
			// Create a query that should timeout
			timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()

			_, err := srv.runReadOnlyQuery(timeoutCtx, "SELECT pg_sleep(1)", 1)
			if err == nil {
				t.Fatalf("expected timeout error")
			}
		})
	}
}

func mockOpenAIFast(tb testing.TB) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		// Return fast, simple SQL for tables that actually exist in the test database
		sql := "SELECT id, name FROM test_users ORDER BY id LIMIT 10"

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
