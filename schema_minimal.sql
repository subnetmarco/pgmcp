-- Minimal Test Schema for PGMCP
-- Includes mixed-case table names to test case sensitivity
-- Reduced data for faster CI execution

-- Drop tables (handle dependencies)
DROP TABLE IF EXISTS reviews CASCADE;
DROP TABLE IF EXISTS order_items CASCADE;
DROP TABLE IF EXISTS orders CASCADE;
DROP TABLE IF EXISTS items CASCADE;
DROP TABLE IF EXISTS "Categories" CASCADE;
DROP TABLE IF EXISTS users CASCADE;

-- Categories table with mixed-case name (tests case sensitivity)
CREATE TABLE "Categories" (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  parent_id INT REFERENCES "Categories"(id),
  slug TEXT UNIQUE NOT NULL,
  description TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Users table
CREATE TABLE users (
  id SERIAL PRIMARY KEY,
  email TEXT UNIQUE NOT NULL,
  first_name TEXT NOT NULL,
  last_name TEXT NOT NULL,
  is_prime BOOLEAN DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Items table
CREATE TABLE items (
  id SERIAL PRIMARY KEY,
  sku TEXT UNIQUE NOT NULL,
  title TEXT NOT NULL,
  description TEXT,
  category_id INT NOT NULL REFERENCES "Categories"(id),
  price_cents INT NOT NULL CHECK (price_cents > 0),
  in_stock INT NOT NULL DEFAULT 0,
  avg_rating DECIMAL(3,2) DEFAULT 0.0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Orders table
CREATE TABLE orders (
  id SERIAL PRIMARY KEY,
  user_id INT NOT NULL REFERENCES users(id),
  status TEXT NOT NULL DEFAULT 'placed',
  total_cents INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Order items (composite primary key - tests AI column assumptions)
CREATE TABLE order_items (
  order_id INT NOT NULL REFERENCES orders(id),
  item_id INT NOT NULL REFERENCES items(id),
  quantity INT NOT NULL CHECK (quantity > 0),
  unit_price_cents INT NOT NULL,
  PRIMARY KEY (order_id, item_id)
);

-- Reviews table
CREATE TABLE reviews (
  id SERIAL PRIMARY KEY,
  item_id INT NOT NULL REFERENCES items(id),
  user_id INT NOT NULL REFERENCES users(id),
  rating INT NOT NULL CHECK (rating >= 1 AND rating <= 5),
  title TEXT,
  content TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(item_id, user_id)
);

-- Invoices table
CREATE TABLE invoices (
  id SERIAL PRIMARY KEY,
  order_id INT NOT NULL REFERENCES orders(id),
  total_cents INT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Create essential indexes
CREATE INDEX idx_items_category ON items(category_id);
CREATE INDEX idx_orders_user ON orders(user_id);
CREATE INDEX idx_reviews_item ON reviews(item_id);

-- Insert minimal test data
INSERT INTO "Categories" (name, parent_id, slug, description) VALUES
('Electronics', NULL, 'electronics', 'Electronic devices'),
('Computers', 1, 'computers', 'Computer equipment'),
('Books', NULL, 'books', 'Books and reading'),
('Fiction', 3, 'fiction', 'Fiction books'),
('Home', NULL, 'home', 'Home products');

INSERT INTO users (email, first_name, last_name, is_prime) VALUES
('alice@test.com', 'Alice', 'Smith', true),
('bob@test.com', 'Bob', 'Jones', false),
('carol@test.com', 'Carol', 'Brown', true);

INSERT INTO items (sku, title, description, category_id, price_cents, in_stock, avg_rating) VALUES
('SKU-001', 'Laptop Pro', 'High-performance laptop', 2, 120000, 5, 4.5),
('SKU-002', 'Good Book', 'A really good book about programming', 4, 2500, 10, 4.8),
('SKU-003', 'USB Cable', 'USB-C cable for charging', 1, 1500, 20, 4.2),
('SKU-004', 'Coffee Maker', 'Automatic coffee maker', 5, 8000, 3, 4.0),
('SKU-005', 'Good Omens', 'Terry Pratchett book with good in title', 4, 1800, 8, 4.9);

INSERT INTO orders (user_id, status, total_cents, created_at) VALUES
(1, 'delivered', 122500, now() - interval '7 days'),
(2, 'placed', 10000, now() - interval '2 days'),
(3, 'shipped', 4300, now() - interval '1 day');

INSERT INTO order_items (order_id, item_id, quantity, unit_price_cents) VALUES
(1, 1, 1, 120000),
(1, 3, 1, 1500),
(1, 5, 1, 1800),
(2, 2, 2, 2500),
(2, 4, 1, 8000),
(3, 3, 2, 1500),
(3, 5, 1, 1800);

INSERT INTO reviews (item_id, user_id, rating, title, content) VALUES
(1, 1, 5, 'Excellent laptop', 'Great performance and build quality'),
(2, 2, 5, 'Good read', 'Really enjoyed this book'),
(3, 1, 4, 'Good cable', 'Works well, good quality'),
(4, 3, 4, 'Good coffee maker', 'Makes good coffee every morning'),
(5, 2, 5, 'Good book', 'Another good book with good content');

INSERT INTO invoices (order_id, total_cents, status, created_at) VALUES
(1, 122500, 'paid', now() - interval '6 days'),
(2, 10000, 'paid', now() - interval '1 day'),
(3, 4300, 'open', now() - interval '1 hour');

-- Update statistics for query planning
ANALYZE;
