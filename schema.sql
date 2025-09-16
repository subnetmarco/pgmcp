-- drop & create
DROP SCHEMA IF EXISTS public CASCADE;
CREATE SCHEMA public;

CREATE TABLE users (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT UNIQUE NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE items (
  id SERIAL PRIMARY KEY,
  sku TEXT UNIQUE NOT NULL,
  title TEXT NOT NULL,
  description TEXT,
  price_cents INT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE orders (
  id SERIAL PRIMARY KEY,
  user_id INT NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  status TEXT NOT NULL DEFAULT 'placed'
);

CREATE TABLE order_items (
  order_id INT NOT NULL REFERENCES orders(id),
  item_id INT NOT NULL REFERENCES items(id),
  quantity INT NOT NULL CHECK (quantity > 0),
  unit_price_cents INT NOT NULL,
  PRIMARY KEY (order_id, item_id)
);

CREATE TABLE invoices (
  id SERIAL PRIMARY KEY,
  order_id INT NOT NULL REFERENCES orders(id),
  total_cents INT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  status TEXT NOT NULL DEFAULT 'open'
);

-- seed
INSERT INTO users (name,email) VALUES
 ('Ada Lovelace','ada@example.com'),
 ('Grace Hopper','grace@example.com'),
 ('Linus Torvalds','linus@example.com');

INSERT INTO items (sku,title,description,price_cents) VALUES
 ('SKU-USB-01','USB-C Cable','1m braided USB-C cable', 999),
 ('SKU-BAT-02','AA Batteries (8-pack)','Long-life alkaline', 1299),
 ('SKU-HDP-03','HDMI Cable','2m HDMI 2.1', 1499),
 ('SKU-KBR-04','Mechanical Keyboard','85-key, brown switches', 8999);

-- orders + order_items
INSERT INTO orders (user_id,status,created_at) VALUES
 (1,'placed', now() - interval '7 days'),
 (2,'placed', now() - interval '3 days'),
 (2,'placed', now() - interval '1 day'),
 (3,'placed', now() - interval '12 hours');

INSERT INTO order_items VALUES
 (1,1,2,999),  -- Ada buys 2 USB-C cables
 (1,2,1,1299), -- Ada buys batteries
 (2,4,1,8999), -- Grace buys keyboard
 (2,2,2,1299), -- Grace buys 2x batteries
 (3,1,1,999),  -- Grace buys USB-C cable
 (4,1,1,999),  -- Linus buys USB-C cable
 (4,3,1,1499); -- Linus buys HDMI cable

INSERT INTO invoices (order_id,total_cents,status,created_at) VALUES
 (1, 2*999 + 1299, 'paid', now() - interval '6 days'),
 (2, 1*8999 + 2*1299, 'paid', now() - interval '3 days'),
 (3, 999, 'open', now() - interval '22 hours'),
 (4, 999 + 1499, 'open', now() - interval '10 hours');

