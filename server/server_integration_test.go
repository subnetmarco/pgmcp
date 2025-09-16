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
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
			Index        int `json:"index"`
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
			ID:      "mock",
			Object:  "chat.completion",
			Model:   "mock",
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
