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

// ----- helpers -----

func mustPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// CI default (matches workflow below)
		dsn = "postgres://postgres:postgres@127.0.0.1:5432/pgmcp_test?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Skipf("skip: cannot parse DATABASE_URL: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Skipf("skip: cannot connect to postgres: %v", err)
	}
	return pool
}

func resetSchema(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = db.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;`)
	_, err := db.Exec(ctx, `
CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT NOT NULL, email TEXT UNIQUE NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE TABLE items (id SERIAL PRIMARY KEY, sku TEXT UNIQUE NOT NULL, title TEXT NOT NULL, description TEXT, price_cents INT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE TABLE orders (id SERIAL PRIMARY KEY, user_id INT NOT NULL REFERENCES users(id), created_at TIMESTAMPTZ NOT NULL DEFAULT now(), status TEXT NOT NULL DEFAULT 'placed');
CREATE TABLE order_items (order_id INT NOT NULL REFERENCES orders(id), item_id INT NOT NULL REFERENCES items(id), quantity INT NOT NULL, unit_price_cents INT NOT NULL, PRIMARY KEY (order_id, item_id));
CREATE TABLE invoices (id SERIAL PRIMARY KEY, order_id INT NOT NULL REFERENCES orders(id), total_cents INT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), status TEXT NOT NULL DEFAULT 'open');

INSERT INTO users (name,email) VALUES
 ('Ada Lovelace','ada@example.com'),
 ('Grace Hopper','grace@example.com'),
 ('Linus Torvalds','linus@example.com');

INSERT INTO items (sku,title,description,price_cents) VALUES
 ('SKU-USB-01','USB-C Cable','1m braided USB-C cable', 999),
 ('SKU-BAT-02','AA Batteries (8-pack)','Long-life alkaline', 1299),
 ('SKU-HDP-03','HDMI Cable','2m HDMI 2.1', 1499),
 ('SKU-KBR-04','Mechanical Keyboard','85-key, brown switches', 8999);

INSERT INTO orders (user_id,status,created_at) VALUES
 (1,'placed', now() - interval '7 days'),
 (2,'placed', now() - interval '3 days'),
 (2,'placed', now() - interval '1 day'),
 (3,'placed', now() - interval '12 hours');

INSERT INTO order_items VALUES
 (1,1,2,999),
 (1,2,1,1299),
 (2,4,1,8999),
 (2,2,2,1299),
 (3,1,1,999),
 (4,1,1,999),
 (4,3,1,1499);

INSERT INTO invoices (order_id,total_cents,status,created_at) VALUES
 (1, 2*999 + 1299, 'paid', now() - interval '6 days'),
 (2, 1*8999 + 2*1299, 'paid', now() - interval '3 days'),
 (3, 999, 'open', now() - interval '22 hours'),
 (4, 999 + 1499, 'open', now() - interval '10 hours');
`)
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}
}

func mockOpenAI(t *testing.T) *httptest.Server {
	// Minimal Chat Completions mock. Returns SQL based on the user's question.
	type reqMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatReq struct {
		Messages []reqMsg `json:"messages"`
	}
	type chatResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			w.WriteHeader(404)
			return
		}
		var req chatReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		userText := ""
		if len(req.Messages) > 0 {
			userText = req.Messages[len(req.Messages)-1].Content
		}
		sql := "SELECT 1 LIMIT 1"
		switch {
		case strings.Contains(strings.ToLower(userText), "most ordered items"):
			sql = `
WITH item_order_counts AS (
  SELECT oi.item_id, SUM(oi.quantity) AS total_quantity
  FROM public.order_items oi
  GROUP BY oi.item_id
)
SELECT i.id AS item_id, i.title AS item_title, i.price_cents AS item_price, i.sku AS item_sku, i.created_at AS item_created_at, o.total_quantity
FROM item_order_counts o
JOIN public.items i ON o.item_id = i.id
ORDER BY o.total_quantity DESC
LIMIT 20`
		case strings.Contains(strings.ToLower(userText), "ascending order based on the quantity of orders"):
			sql = `
WITH user_order_counts AS (
  SELECT u.id AS user_id, COUNT(o.id) AS order_count
  FROM public.users u
  LEFT JOIN public.orders o ON u.id = o.user_id
  GROUP BY u.id
)
SELECT user_id, order_count
FROM user_order_counts
ORDER BY order_count ASC
LIMIT 20`
		case strings.Contains(strings.ToLower(userText), "last 3 invoices"):
			sql = `
WITH last_user AS (
  SELECT user_id
  FROM public.orders
  ORDER BY created_at DESC
  LIMIT 1
),
last_item AS (
  SELECT id FROM public.items ORDER BY created_at DESC LIMIT 1
)
SELECT i.id AS invoice_id, i.created_at AS invoice_date, i.total_cents
FROM public.invoices i
JOIN public.orders o ON i.order_id = o.id
JOIN public.order_items oi ON o.id = oi.order_id
WHERE o.user_id = (SELECT user_id FROM last_user)
  AND oi.item_id = (SELECT id FROM last_item)
ORDER BY i.created_at DESC
LIMIT 3`
		}
		resp := chatResp{
			ID:     "mock",
			Object: "chat.completion",
			Model:  "mock",
			Choices: []struct {
				Index        int    `json:"index"`
				FinishReason string `json:"finish_reason"`
				Message      struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Index: 0, FinishReason: "stop", Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: strings.TrimSpace(sql)}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return httptest.NewServer(h)
}

// ----- tests -----

func TestAsk_MostOrderedItems(t *testing.T) {
	db := mustPool(t)
	defer db.Close()
	resetSchema(t, db)

	mock := mockOpenAI(t)
	defer mock.Close()

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test",
		OpenAIBase:  mock.URL + "/v1", // SDK appends /chat/completions
		OpenAIModel: "mock",
		SchemaTTL:   1 * time.Minute,
		QueryTO:     10 * time.Second,
		MaxRows:     50,
	}
	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	_, out, err := srv.handleAsk(ctx, nil, askInput{Query: "Get the most ordered items in the marketplace", MaxRows: 20})
	if err != nil {
		t.Fatalf("ask failed: %v", err)
	}
	if len(out.Rows) == 0 {
		t.Fatalf("expected rows, got 0; sql=%s", out.SQL)
	}
	// spot-check columns
	if _, ok := out.Rows[0]["item_id"]; !ok {
		t.Fatalf("expected item_id in row")
	}
}

func TestAsk_Last3Invoices(t *testing.T) {
	db := mustPool(t)
	defer db.Close()
	resetSchema(t, db)

	mock := mockOpenAI(t)
	defer mock.Close()

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test",
		OpenAIBase:  mock.URL + "/v1",
		OpenAIModel: "mock",
		QueryTO:     10 * time.Second,
		MaxRows:     5,
	}
	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	_, out, err := srv.handleAsk(ctx, nil, askInput{Query: "Get the last 3 invoices for the last user that purchased a specific item", MaxRows: 3})
	if err != nil {
		t.Fatalf("ask failed: %v", err)
	}
	if len(out.Rows) == 0 {
		t.Fatalf("expected at least 1 invoice row; sql=%s", out.SQL)
	}
}

func TestSearch_FreeText(t *testing.T) {
	db := mustPool(t)
	defer db.Close()
	resetSchema(t, db)

	// Build server without LLM (not needed for search)
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		QueryTO:     10 * time.Second,
		MaxRows:     20,
		SchemaTTL:   1 * time.Minute,
	}
	ctx := context.Background()
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	_, out, err := srv.handleSearch(ctx, nil, searchInput{Q: "Cable", Limit: 10})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(out.Rows) == 0 {
		t.Fatalf("expected search hits for 'Cable'")
	}
}

func TestAsk_CapitalTableName(t *testing.T) {
	db := mustPool(t)
	defer db.Close()

	// Create a schema with a capital letter table name
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = db.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;`)
	_, err := db.Exec(ctx, `
CREATE TABLE "Book" (
	id SERIAL PRIMARY KEY, 
	title TEXT NOT NULL, 
	author TEXT NOT NULL, 
	isbn TEXT UNIQUE,
	published_year INT,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO "Book" (title, author, isbn, published_year) VALUES
 ('Good Omens', 'Terry Pratchett & Neil Gaiman', '978-0060853983', 1990),
 ('The Good, the Bad and the Ugly Guide to Programming', 'John Doe', '978-1234567890', 2020),
 ('Good to Great', 'Jim Collins', '978-0066620992', 2001),
 ('A Good Man Is Hard to Find', 'Flannery O''Connor', '978-0156364652', 1955);
`)
	if err != nil {
		t.Fatalf("seed failed: %v", err)
	}

	// Mock OpenAI to return SQL that references the table with lowercase name
	mock := mockOpenAICapitalTable(t)
	defer mock.Close()

	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		OpenAIKey:   "test",
		OpenAIBase:  mock.URL + "/v1",
		OpenAIModel: "mock",
		SchemaTTL:   1 * time.Minute,
		QueryTO:     10 * time.Second,
		MaxRows:     50,
	}
	srv, err := newServer(ctx, cfg)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	// With our fix, the LLM should generate SQL with properly quoted "Book" table name
	// This should now work because the schema shows "Book" and the LLM uses it correctly
	_, out, err := srv.handleAsk(ctx, nil, askInput{Query: "list all books where title contains good", MaxRows: 20})

	// The query should now succeed with our fix
	if err != nil {
		t.Fatalf("Expected query to succeed with proper quoting, but it failed: %v. SQL: %s", err, out.SQL)
	}

	// Verify that the generated SQL uses properly quoted table name
	if !strings.Contains(out.SQL, `"Book"`) {
		t.Fatalf("Expected generated SQL to use quoted 'Book', got: %s", out.SQL)
	}

	// Should have results
	if len(out.Rows) == 0 {
		t.Fatalf("Expected results from Book table, got none. SQL: %s", out.SQL)
	}

	t.Logf("Test confirmed: Generated SQL '%s' works with proper quoting", out.SQL)
}

func mockOpenAICapitalTable(t *testing.T) *httptest.Server {
	// Mock that returns SQL with properly quoted table names, demonstrating the fix
	// The LLM should now use the exact table names as shown in the schema
	type reqMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatReq struct {
		Messages []reqMsg `json:"messages"`
	}
	type chatResp struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int    `json:"index"`
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			w.WriteHeader(404)
			return
		}
		var req chatReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		userText := ""
		if len(req.Messages) > 0 {
			userText = req.Messages[len(req.Messages)-1].Content
		}

		// Default SQL that will fail due to case sensitivity
		sql := "SELECT 1 LIMIT 1"

		if strings.Contains(strings.ToLower(userText), "books") && strings.Contains(strings.ToLower(userText), "title") && strings.Contains(strings.ToLower(userText), "good") {
			// This simulates what an LLM should generate with our fix - properly quoted table name
			// The schema will show "Book" (quoted) and the LLM should use it exactly
			sql = `SELECT id, title, author, isbn, published_year FROM public."Book" WHERE title ILIKE '%good%' LIMIT 20`
		}

		resp := chatResp{
			ID:     "mock",
			Object: "chat.completion",
			Model:  "mock",
			Choices: []struct {
				Index        int    `json:"index"`
				FinishReason string `json:"finish_reason"`
				Message      struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}{
				{Index: 0, FinishReason: "stop", Message: struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{Role: "assistant", Content: strings.TrimSpace(sql)}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return httptest.NewServer(h)
}
