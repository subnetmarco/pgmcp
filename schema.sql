-- Amazon-like Marketplace Schema
-- Drop all tables individually to handle dependencies properly
DROP TABLE IF EXISTS product_views CASCADE;
DROP TABLE IF EXISTS invoices CASCADE;
DROP TABLE IF EXISTS wishlist_items CASCADE;
DROP TABLE IF EXISTS wishlists CASCADE;
DROP TABLE IF EXISTS cart_items CASCADE;
DROP TABLE IF EXISTS order_items CASCADE;
DROP TABLE IF EXISTS orders CASCADE;
DROP TABLE IF EXISTS item_sellers CASCADE;
DROP TABLE IF EXISTS sellers CASCADE;
DROP TABLE IF EXISTS reviews CASCADE;
DROP TABLE IF EXISTS items CASCADE;
DROP TABLE IF EXISTS brands CASCADE;
DROP TABLE IF EXISTS categories CASCADE;
DROP TABLE IF EXISTS users CASCADE;

-- Categories table (hierarchical categories)
CREATE TABLE categories (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  parent_id INT REFERENCES categories(id),
  slug TEXT UNIQUE NOT NULL,
  description TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Brands table
CREATE TABLE brands (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  description TEXT,
  logo_url TEXT,
  website TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Enhanced users table
CREATE TABLE users (
  id SERIAL PRIMARY KEY,
  email TEXT UNIQUE NOT NULL,
  first_name TEXT NOT NULL,
  last_name TEXT NOT NULL,
  phone TEXT,
  date_of_birth DATE,
  city TEXT,
  state TEXT,
  country TEXT DEFAULT 'US',
  zip_code TEXT,
  is_prime BOOLEAN DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_login TIMESTAMPTZ
);

-- Enhanced items/products table
CREATE TABLE items (
  id SERIAL PRIMARY KEY,
  sku TEXT UNIQUE NOT NULL,
  title TEXT NOT NULL,
  description TEXT,
  brand_id INT REFERENCES brands(id),
  category_id INT NOT NULL REFERENCES categories(id),
  price_cents INT NOT NULL CHECK (price_cents > 0),
  list_price_cents INT, -- original price before discount
  weight_grams INT,
  dimensions_cm TEXT, -- "10x5x2"
  color TEXT,
  size TEXT,
  material TEXT,
  is_prime_eligible BOOLEAN DEFAULT false,
  in_stock INT NOT NULL DEFAULT 0,
  avg_rating DECIMAL(3,2) DEFAULT 0.0,
  review_count INT DEFAULT 0,
  image_urls TEXT[], -- array of image URLs
  tags TEXT[], -- searchable tags
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Product reviews
CREATE TABLE reviews (
  id SERIAL PRIMARY KEY,
  item_id INT NOT NULL REFERENCES items(id),
  user_id INT NOT NULL REFERENCES users(id),
  rating INT NOT NULL CHECK (rating >= 1 AND rating <= 5),
  title TEXT,
  content TEXT,
  verified_purchase BOOLEAN DEFAULT false,
  helpful_votes INT DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(item_id, user_id) -- one review per user per item
);

-- Sellers table (marketplace sellers)
CREATE TABLE sellers (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT UNIQUE NOT NULL,
  business_name TEXT,
  rating DECIMAL(3,2) DEFAULT 0.0,
  total_sales INT DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Item sellers (items can have multiple sellers)
CREATE TABLE item_sellers (
  item_id INT NOT NULL REFERENCES items(id),
  seller_id INT NOT NULL REFERENCES sellers(id),
  price_cents INT NOT NULL,
  condition TEXT DEFAULT 'new', -- new, used, refurbished
  shipping_cost_cents INT DEFAULT 0,
  is_prime BOOLEAN DEFAULT false,
  in_stock INT DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (item_id, seller_id)
);

-- Enhanced orders table
CREATE TABLE orders (
  id SERIAL PRIMARY KEY,
  user_id INT NOT NULL REFERENCES users(id),
  status TEXT NOT NULL DEFAULT 'placed', -- placed, processing, shipped, delivered, cancelled
  subtotal_cents INT NOT NULL DEFAULT 0,
  tax_cents INT NOT NULL DEFAULT 0,
  shipping_cents INT NOT NULL DEFAULT 0,
  total_cents INT NOT NULL DEFAULT 0,
  shipping_address TEXT,
  billing_address TEXT,
  payment_method TEXT DEFAULT 'credit_card',
  tracking_number TEXT,
  estimated_delivery DATE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  shipped_at TIMESTAMPTZ,
  delivered_at TIMESTAMPTZ
);

-- Enhanced order items
CREATE TABLE order_items (
  order_id INT NOT NULL REFERENCES orders(id),
  item_id INT NOT NULL REFERENCES items(id),
  seller_id INT NOT NULL REFERENCES sellers(id),
  quantity INT NOT NULL CHECK (quantity > 0),
  unit_price_cents INT NOT NULL,
  condition TEXT DEFAULT 'new',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (order_id, item_id, seller_id)
);

-- Shopping cart
CREATE TABLE cart_items (
  user_id INT NOT NULL REFERENCES users(id),
  item_id INT NOT NULL REFERENCES items(id),
  seller_id INT NOT NULL REFERENCES sellers(id),
  quantity INT NOT NULL CHECK (quantity > 0),
  added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, item_id, seller_id)
);

-- Wishlists
CREATE TABLE wishlists (
  id SERIAL PRIMARY KEY,
  user_id INT NOT NULL REFERENCES users(id),
  name TEXT NOT NULL DEFAULT 'My Wishlist',
  is_public BOOLEAN DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE wishlist_items (
  wishlist_id INT NOT NULL REFERENCES wishlists(id),
  item_id INT NOT NULL REFERENCES items(id),
  added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (wishlist_id, item_id)
);

-- Enhanced invoices
CREATE TABLE invoices (
  id SERIAL PRIMARY KEY,
  order_id INT NOT NULL REFERENCES orders(id),
  invoice_number TEXT UNIQUE NOT NULL,
  subtotal_cents INT NOT NULL,
  tax_cents INT NOT NULL,
  shipping_cents INT NOT NULL,
  total_cents INT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending', -- pending, paid, failed, refunded
  payment_method TEXT,
  transaction_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  paid_at TIMESTAMPTZ,
  due_date TIMESTAMPTZ
);

-- Product search/recommendations
CREATE TABLE product_views (
  user_id INT REFERENCES users(id),
  item_id INT NOT NULL REFERENCES items(id),
  viewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  session_id TEXT,
  referrer TEXT
);

-- Create indexes for performance
CREATE INDEX idx_items_category ON items(category_id);
CREATE INDEX idx_items_brand ON items(brand_id);
CREATE INDEX idx_items_price ON items(price_cents);
CREATE INDEX idx_items_rating ON items(avg_rating);
CREATE INDEX idx_items_created ON items(created_at);
CREATE INDEX idx_items_title_search ON items USING gin(to_tsvector('english', title));
CREATE INDEX idx_items_description_search ON items USING gin(to_tsvector('english', description));

CREATE INDEX idx_orders_user ON orders(user_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_created ON orders(created_at);

CREATE INDEX idx_reviews_item ON reviews(item_id);
CREATE INDEX idx_reviews_user ON reviews(user_id);
CREATE INDEX idx_reviews_rating ON reviews(rating);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_created ON users(created_at);
CREATE INDEX idx_users_prime ON users(is_prime);

-- ============================================================================
-- SEED DATA - Amazon-like marketplace with thousands of entries
-- ============================================================================

-- Insert categories (hierarchical)
INSERT INTO categories (name, parent_id, slug, description) VALUES
('Electronics', NULL, 'electronics', 'Electronic devices and accessories'),
('Computers', 1, 'computers', 'Laptops, desktops, and computer accessories'),
('Laptops', 2, 'laptops', 'Portable computers'),
('Desktops', 2, 'desktops', 'Desktop computers'),
('Accessories', 2, 'computer-accessories', 'Computer peripherals and accessories'),
('Mobile Phones', 1, 'mobile-phones', 'Smartphones and mobile devices'),
('Audio', 1, 'audio', 'Headphones, speakers, and audio equipment'),
('Gaming', 1, 'gaming', 'Gaming consoles, games, and accessories'),

('Home & Kitchen', NULL, 'home-kitchen', 'Home and kitchen products'),
('Furniture', 9, 'furniture', 'Home furniture'),
('Kitchen', 9, 'kitchen', 'Kitchen appliances and tools'),
('Decor', 9, 'decor', 'Home decoration items'),

('Clothing', NULL, 'clothing', 'Apparel and fashion'),
('Men', 13, 'mens-clothing', 'Men''s clothing'),
('Women', 13, 'womens-clothing', 'Women''s clothing'),
('Shoes', 13, 'shoes', 'Footwear for all'),

('Books', NULL, 'books', 'Books and reading materials'),
('Fiction', 17, 'fiction', 'Fiction books'),
('Non-Fiction', 17, 'non-fiction', 'Non-fiction books'),
('Technical', 17, 'technical', 'Technical and educational books'),

('Sports', NULL, 'sports', 'Sports and outdoor equipment'),
('Fitness', 21, 'fitness', 'Fitness equipment'),
('Outdoor', 21, 'outdoor', 'Outdoor and camping gear');

-- Insert brands
INSERT INTO brands (name, description, website) VALUES
('Apple', 'Technology company known for iPhones, MacBooks, and more', 'https://apple.com'),
('Samsung', 'Electronics manufacturer', 'https://samsung.com'),
('Dell', 'Computer manufacturer', 'https://dell.com'),
('HP', 'Technology company', 'https://hp.com'),
('Lenovo', 'Computer manufacturer', 'https://lenovo.com'),
('Sony', 'Electronics and entertainment', 'https://sony.com'),
('Microsoft', 'Software and hardware company', 'https://microsoft.com'),
('Google', 'Technology company', 'https://google.com'),
('Amazon', 'E-commerce and cloud computing', 'https://amazon.com'),
('Nike', 'Athletic footwear and apparel', 'https://nike.com'),
('Adidas', 'Athletic footwear and apparel', 'https://adidas.com'),
('Levi''s', 'Denim and casual wear', 'https://levis.com'),
('KitchenAid', 'Kitchen appliances', 'https://kitchenaid.com'),
('Cuisinart', 'Kitchen appliances', 'https://cuisinart.com'),
('IKEA', 'Furniture and home goods', 'https://ikea.com'),
('Penguin Random House', 'Book publisher', 'https://penguinrandomhouse.com'),
('HarperCollins', 'Book publisher', 'https://harpercollins.com'),
('Fitbit', 'Fitness tracking devices', 'https://fitbit.com'),
('GoPro', 'Action cameras', 'https://gopro.com'),
('Bose', 'Audio equipment', 'https://bose.com');

-- Generate thousands of users
INSERT INTO users (email, first_name, last_name, phone, city, state, country, zip_code, is_prime, created_at, last_login)
SELECT 
  'user' || generate_series || '@email.com',
  (ARRAY['John', 'Jane', 'Michael', 'Sarah', 'David', 'Emily', 'James', 'Lisa', 'Robert', 'Maria', 'William', 'Jennifer', 'Richard', 'Linda', 'Charles', 'Patricia', 'Joseph', 'Elizabeth', 'Thomas', 'Barbara'])[1 + (random() * 19)::int],
  (ARRAY['Smith', 'Johnson', 'Williams', 'Brown', 'Jones', 'Garcia', 'Miller', 'Davis', 'Rodriguez', 'Martinez', 'Hernandez', 'Lopez', 'Gonzalez', 'Wilson', 'Anderson', 'Thomas', 'Taylor', 'Moore', 'Jackson', 'Martin'])[1 + (random() * 19)::int],
  '+1' || lpad((random() * 9999999999)::bigint::text, 10, '0'),
  (ARRAY['New York', 'Los Angeles', 'Chicago', 'Houston', 'Phoenix', 'Philadelphia', 'San Antonio', 'San Diego', 'Dallas', 'San Jose', 'Austin', 'Jacksonville', 'Fort Worth', 'Columbus', 'Charlotte', 'San Francisco', 'Indianapolis', 'Seattle', 'Denver', 'Boston'])[1 + (random() * 19)::int],
  (ARRAY['NY', 'CA', 'IL', 'TX', 'AZ', 'PA', 'TX', 'CA', 'TX', 'CA', 'TX', 'FL', 'TX', 'OH', 'NC', 'CA', 'IN', 'WA', 'CO', 'MA'])[1 + (random() * 19)::int],
  'US',
  lpad((random() * 99999)::int::text, 5, '0'),
  random() > 0.7, -- 30% are Prime members
  now() - (random() * interval '2 years'),
  now() - (random() * interval '30 days')
FROM generate_series(1, 5000);

-- Generate sellers
INSERT INTO sellers (name, email, business_name, rating, total_sales, created_at)
SELECT 
  (ARRAY['TechStore', 'ElectroMart', 'GadgetHub', 'HomeGoods', 'FashionPlus', 'BookWorld', 'SportZone', 'KitchenPro', 'StyleShop', 'AudioMax'])[1 + (random() * 9)::int] || ' ' || generate_series,
  'seller' || generate_series || '@business.com',
  (ARRAY['Tech Solutions LLC', 'Electronics Emporium', 'Gadget Galaxy Inc', 'Home & Living Co', 'Fashion Forward LLC', 'Book Paradise', 'Sports Authority', 'Kitchen Masters', 'Style Central', 'Audio Experts'])[1 + (random() * 9)::int],
  3.0 + random() * 2.0, -- Rating 3-5
  (random() * 10000)::int, -- Total sales
  now() - (random() * interval '3 years')
FROM generate_series(1, 500);

-- Generate thousands of items across categories
INSERT INTO items (
  sku, title, description, brand_id, category_id, price_cents, list_price_cents, 
  weight_grams, color, size, is_prime_eligible, in_stock, avg_rating, review_count,
  created_at
)
SELECT 
  'SKU-' || category_id || '-' || lpad(row_number() OVER (PARTITION BY category_id)::text, 6, '0'),
  CASE category_id
    -- Electronics
    WHEN 3 THEN (ARRAY['MacBook Pro', 'MacBook Air', 'ThinkPad X1', 'Dell XPS', 'HP Spectre', 'Surface Laptop', 'Gaming Laptop', 'Ultrabook', 'Chromebook', 'Workstation'])[1 + (random() * 9)::int] || ' ' || (15 + random() * 5)::int || 'inch'
    WHEN 4 THEN (ARRAY['Gaming PC', 'Office Desktop', 'Workstation', 'Mini PC', 'All-in-One', 'Custom Build'])[1 + (random() * 5)::int] || ' ' || (ARRAY['Intel i5', 'Intel i7', 'AMD Ryzen 5', 'AMD Ryzen 7'])[1 + (random() * 3)::int]
    WHEN 5 THEN (ARRAY['Wireless Mouse', 'Mechanical Keyboard', 'USB-C Hub', 'Monitor', 'Webcam', 'Speakers', 'Headset', 'Cable', 'Adapter', 'Stand'])[1 + (random() * 9)::int]
    WHEN 6 THEN (ARRAY['iPhone', 'Galaxy S', 'Pixel', 'OnePlus'])[1 + (random() * 3)::int] || ' ' || (10 + random() * 5)::int
    WHEN 7 THEN (ARRAY['Wireless Headphones', 'Bluetooth Speaker', 'Soundbar', 'Earbuds', 'Studio Monitor'])[1 + (random() * 4)::int]
    WHEN 8 THEN (ARRAY['PlayStation 5', 'Xbox Series X', 'Nintendo Switch', 'Gaming Chair', 'Controller', 'Gaming Headset'])[1 + (random() * 5)::int]
    
    -- Home & Kitchen  
    WHEN 10 THEN (ARRAY['Sofa', 'Chair', 'Table', 'Bed', 'Dresser', 'Bookshelf', 'Desk', 'Cabinet'])[1 + (random() * 7)::int] || ' ' || (ARRAY['Modern', 'Classic', 'Rustic', 'Contemporary'])[1 + (random() * 3)::int]
    WHEN 11 THEN (ARRAY['Blender', 'Coffee Maker', 'Toaster', 'Microwave', 'Air Fryer', 'Stand Mixer', 'Food Processor', 'Rice Cooker'])[1 + (random() * 7)::int]
    WHEN 12 THEN (ARRAY['Wall Art', 'Lamp', 'Vase', 'Candle', 'Mirror', 'Plant Pot', 'Throw Pillow', 'Rug'])[1 + (random() * 7)::int]
    
    -- Clothing
    WHEN 14 THEN (ARRAY['T-Shirt', 'Jeans', 'Jacket', 'Sweater', 'Polo Shirt', 'Pants', 'Shorts', 'Hoodie'])[1 + (random() * 7)::int] || ' Men''s'
    WHEN 15 THEN (ARRAY['Dress', 'Blouse', 'Jeans', 'Jacket', 'Sweater', 'Skirt', 'Pants', 'Top'])[1 + (random() * 7)::int] || ' Women''s'
    WHEN 16 THEN (ARRAY['Sneakers', 'Boots', 'Sandals', 'Dress Shoes', 'Running Shoes', 'Heels', 'Flats'])[1 + (random() * 6)::int]
    
    -- Books
    WHEN 18 THEN (ARRAY['The Great', 'Adventures of', 'Mystery of', 'Love in', 'War and', 'Tales from', 'Journey to', 'Secret of'])[1 + (random() * 7)::int] || ' ' || (ARRAY['Tomorrow', 'Yesterday', 'Paradise', 'Darkness', 'Light', 'Freedom', 'Hope', 'Dreams'])[1 + (random() * 7)::int]
    WHEN 19 THEN (ARRAY['How to', 'The Art of', 'Mastering', 'Guide to', 'Understanding'])[1 + (random() * 4)::int] || ' ' || (ARRAY['Success', 'Leadership', 'Innovation', 'Productivity', 'Mindfulness', 'Health', 'Wealth'])[1 + (random() * 6)::int]
    WHEN 20 THEN (ARRAY['Programming', 'Data Science', 'Machine Learning', 'Web Development', 'System Design', 'DevOps', 'Cybersecurity'])[1 + (random() * 6)::int] || ' ' || (ARRAY['Handbook', 'Guide', 'Mastery', 'Fundamentals', 'Advanced'])[1 + (random() * 4)::int]
    
    -- Sports
    WHEN 22 THEN (ARRAY['Treadmill', 'Dumbbells', 'Yoga Mat', 'Exercise Bike', 'Resistance Bands', 'Kettlebell', 'Pull-up Bar'])[1 + (random() * 6)::int]
    WHEN 23 THEN (ARRAY['Tent', 'Sleeping Bag', 'Backpack', 'Hiking Boots', 'Camping Stove', 'Water Bottle', 'Flashlight'])[1 + (random() * 6)::int]
    
    ELSE 'Product ' || generate_series
  END,
  
  CASE category_id
    WHEN 3 THEN 'High-performance laptop with latest processor and graphics'
    WHEN 4 THEN 'Powerful desktop computer for work and gaming'
    WHEN 5 THEN 'Essential computer accessory for productivity'
    WHEN 6 THEN 'Latest smartphone with advanced camera and features'
    WHEN 7 THEN 'Premium audio equipment for music lovers'
    WHEN 8 THEN 'Gaming equipment for ultimate gaming experience'
    WHEN 10 THEN 'Stylish and comfortable furniture for your home'
    WHEN 11 THEN 'High-quality kitchen appliance for cooking enthusiasts'
    WHEN 12 THEN 'Beautiful home decoration to enhance your living space'
    WHEN 14 THEN 'Comfortable and stylish men''s clothing'
    WHEN 15 THEN 'Fashionable and elegant women''s clothing'
    WHEN 16 THEN 'Comfortable and durable footwear'
    WHEN 18 THEN 'Captivating fiction story that will keep you engaged'
    WHEN 19 THEN 'Informative non-fiction book with practical insights'
    WHEN 20 THEN 'Comprehensive technical guide for professionals'
    WHEN 22 THEN 'Professional fitness equipment for home workouts'
    WHEN 23 THEN 'Durable outdoor gear for adventures'
    ELSE 'High-quality product with excellent features'
  END,
  
  1 + (random() * 19)::int, -- brand_id
  category_id,
  
  -- Price based on category
  CASE category_id
    WHEN 3 THEN 80000 + (random() * 200000)::int -- Laptops: $800-2800
    WHEN 4 THEN 60000 + (random() * 300000)::int -- Desktops: $600-3600
    WHEN 5 THEN 1000 + (random() * 50000)::int   -- Accessories: $10-510
    WHEN 6 THEN 30000 + (random() * 120000)::int -- Phones: $300-1500
    WHEN 7 THEN 5000 + (random() * 50000)::int   -- Audio: $50-550
    WHEN 8 THEN 20000 + (random() * 80000)::int  -- Gaming: $200-1000
    WHEN 10 THEN 15000 + (random() * 200000)::int -- Furniture: $150-2150
    WHEN 11 THEN 3000 + (random() * 50000)::int  -- Kitchen: $30-530
    WHEN 12 THEN 1000 + (random() * 20000)::int  -- Decor: $10-210
    WHEN 14 THEN 2000 + (random() * 15000)::int  -- Men's: $20-170
    WHEN 15 THEN 2500 + (random() * 20000)::int  -- Women's: $25-225
    WHEN 16 THEN 5000 + (random() * 30000)::int  -- Shoes: $50-350
    WHEN 18 THEN 1000 + (random() * 5000)::int   -- Fiction: $10-60
    WHEN 19 THEN 1500 + (random() * 8000)::int   -- Non-fiction: $15-95
    WHEN 20 THEN 3000 + (random() * 15000)::int  -- Technical: $30-180
    WHEN 22 THEN 5000 + (random() * 100000)::int -- Fitness: $50-1050
    WHEN 23 THEN 2000 + (random() * 50000)::int  -- Outdoor: $20-520
    ELSE 1000 + (random() * 10000)::int
  END,
  
  -- List price (10-30% higher than sale price)  
  CASE category_id
    WHEN 3 THEN (80000 + (random() * 200000)::int) * (1.1 + random() * 0.2)
    WHEN 4 THEN (60000 + (random() * 300000)::int) * (1.1 + random() * 0.2)
    ELSE (1000 + (random() * 50000)::int) * (1.1 + random() * 0.2)
  END,
  
  -- Weight in grams
  CASE category_id
    WHEN 3 THEN 1000 + (random() * 2000)::int -- Laptops: 1-3kg
    WHEN 4 THEN 5000 + (random() * 10000)::int -- Desktops: 5-15kg
    WHEN 6 THEN 150 + (random() * 200)::int    -- Phones: 150-350g
    ELSE 100 + (random() * 1000)::int
  END,
  
  (ARRAY['Black', 'White', 'Silver', 'Gray', 'Blue', 'Red', 'Green', 'Gold', 'Rose Gold', 'Purple'])[1 + (random() * 9)::int],
  (ARRAY['XS', 'S', 'M', 'L', 'XL', 'XXL', 'One Size'])[1 + (random() * 6)::int],
  random() > 0.4, -- 60% Prime eligible
  -- Stock quantity with realistic variability (0-25 range)
  CASE 
    WHEN random() < 0.15 THEN 0                           -- 15% out of stock
    WHEN random() < 0.30 THEN 1 + (random() * 2)::int    -- 15% low stock: 1-3
    WHEN random() < 0.60 THEN 3 + (random() * 7)::int    -- 30% moderate stock: 3-10
    WHEN random() < 0.85 THEN 10 + (random() * 10)::int  -- 25% good stock: 10-20
    ELSE 20 + (random() * 5)::int                         -- 15% high stock: 20-25
  END,
  1.0 + random() * 4.0, -- Rating 1-5 (will be updated based on actual reviews)
  0, -- Review count (will be updated based on actual reviews)
  now() - (random() * interval '1 year')
FROM generate_series(1, 100) -- 100 items per category
CROSS JOIN (SELECT id as category_id FROM categories WHERE parent_id IS NOT NULL) cat;

-- Link items to sellers with realistic variability
-- Some items have many sellers (popular), others have few or one seller
WITH item_seller_distribution AS (
  SELECT 
    i.id as item_id,
    CASE 
      WHEN random() < 0.10 THEN 8 + (random() * 17)::int  -- 10% popular items: 8-25 sellers
      WHEN random() < 0.25 THEN 5 + (random() * 10)::int  -- 15% common items: 5-15 sellers
      WHEN random() < 0.50 THEN 2 + (random() * 6)::int   -- 25% moderate items: 2-8 sellers
      WHEN random() < 0.80 THEN 1 + (random() * 2)::int   -- 30% niche items: 1-3 sellers
      ELSE 1                                               -- 20% exclusive items: 1 seller only
    END as seller_count
  FROM items i
)
INSERT INTO item_sellers (item_id, seller_id, price_cents, condition, shipping_cost_cents, is_prime, in_stock)
SELECT 
  isd.item_id,
  s.id,
  i.price_cents + (-500 + random() * 1000)::int, -- Price variation
  (ARRAY['new', 'used', 'refurbished'])[1 + (random() * 2)::int],
  CASE WHEN random() > 0.3 THEN 0 ELSE 500 + (random() * 1500)::int END, -- Free shipping 70% of time
  random() > 0.5,
  -- Stock variability (0-25 range)
  CASE 
    WHEN random() < 0.20 THEN 0                           -- 20% out of stock
    WHEN random() < 0.35 THEN 1 + (random() * 2)::int    -- 15% low stock: 1-3
    WHEN random() < 0.65 THEN 3 + (random() * 7)::int    -- 30% moderate stock: 3-10
    WHEN random() < 0.85 THEN 10 + (random() * 10)::int  -- 20% good stock: 10-20
    ELSE 20 + (random() * 5)::int                         -- 15% high stock: 20-25
  END
FROM item_seller_distribution isd
CROSS JOIN LATERAL (
  SELECT id FROM sellers ORDER BY random() LIMIT isd.seller_count
) s
JOIN items i ON isd.item_id = i.id;

-- Generate realistic orders with power law distribution (some users order a lot, most order little)
-- First, generate orders with weighted user distribution
WITH user_weights AS (
  SELECT 
    id as user_id,
    CASE 
      WHEN random() < 0.05 THEN 15 + (random() * 10)::int  -- 5% power users: 15-25 orders
      WHEN random() < 0.15 THEN 8 + (random() * 7)::int   -- 10% frequent users: 8-15 orders  
      WHEN random() < 0.40 THEN 3 + (random() * 5)::int   -- 25% regular users: 3-8 orders
      WHEN random() < 0.70 THEN 1 + (random() * 2)::int   -- 30% occasional users: 1-3 orders
      ELSE 0 + (random() * 1)::int                         -- 30% rare users: 0-1 orders
    END as order_count
  FROM users
),
order_distribution AS (
  SELECT 
    user_id,
    generate_series(1, order_count) as order_num
  FROM user_weights
  WHERE order_count > 0
)
INSERT INTO orders (
  user_id, status, subtotal_cents, tax_cents, shipping_cents, total_cents,
  shipping_address, payment_method, tracking_number, estimated_delivery,
  created_at, shipped_at, delivered_at
)
SELECT 
  od.user_id,
  (ARRAY['placed', 'processing', 'shipped', 'delivered', 'delivered', 'delivered'])[1 + (random() * 5)::int], -- 50% delivered
  subtotal,
  (subtotal * 0.08)::int, -- 8% tax
  CASE WHEN random() > 0.6 THEN 0 ELSE 500 + (random() * 1500)::int END, -- Free shipping 60% of time
  subtotal + (subtotal * 0.08)::int + CASE WHEN random() > 0.6 THEN 0 ELSE 500 + (random() * 1500)::int END,
  (random() * 999)::int || ' Main St, ' || 
  (ARRAY['New York', 'Los Angeles', 'Chicago', 'Houston', 'Phoenix'])[1 + (random() * 4)::int] || ', ' ||
  (ARRAY['NY', 'CA', 'IL', 'TX', 'AZ'])[1 + (random() * 4)::int] || ' ' ||
  lpad((random() * 99999)::int::text, 5, '0'),
  (ARRAY['credit_card', 'debit_card', 'paypal', 'apple_pay', 'google_pay'])[1 + (random() * 4)::int],
  CASE WHEN random() > 0.3 THEN '1Z' || upper(substring(md5(random()::text), 1, 16)) ELSE NULL END,
  CASE WHEN random() > 0.2 THEN (now() + interval '2 days' + (random() * interval '10 days')) ELSE NULL END,
  order_date,
  CASE WHEN random() > 0.4 THEN order_date + interval '1 day' + (random() * interval '3 days') ELSE NULL END,
  CASE WHEN random() > 0.5 THEN order_date + interval '3 days' + (random() * interval '7 days') ELSE NULL END
FROM order_distribution od
CROSS JOIN (
  SELECT 
    2000 + (random() * 50000)::int as subtotal, -- $20-520 orders
    now() - (random() * interval '1 year') as order_date
) order_data;

-- Generate order items with realistic item popularity distribution
-- Some items are bestsellers (ordered frequently), others rarely ordered
WITH item_popularity AS (
  SELECT 
    id,
    CASE 
      WHEN random() < 0.05 THEN 20 + (random() * 5)::int  -- 5% bestsellers: appear in 20-25 orders
      WHEN random() < 0.15 THEN 12 + (random() * 8)::int  -- 10% popular: 12-20 orders
      WHEN random() < 0.35 THEN 5 + (random() * 7)::int   -- 20% moderate: 5-12 orders
      WHEN random() < 0.60 THEN 2 + (random() * 3)::int   -- 25% occasional: 2-5 orders
      WHEN random() < 0.85 THEN 0 + (random() * 2)::int   -- 25% rare: 0-2 orders
      ELSE 0                                               -- 15% never ordered
    END as order_frequency
  FROM items
),
item_orders AS (
  SELECT 
    ip.id as item_id,
    o.id as order_id,
    row_number() OVER (PARTITION BY ip.id ORDER BY random()) as item_order_num
  FROM item_popularity ip
  CROSS JOIN orders o
  WHERE ip.order_frequency > 0
    AND random() < (ip.order_frequency::float / 25.0) -- Probability based on popularity
)
INSERT INTO order_items (order_id, item_id, seller_id, quantity, unit_price_cents, condition)
SELECT 
  io.order_id,
  io.item_id,
  s.seller_id,
  1 + (random() * 3)::int, -- 1-4 quantity
  i.price_cents + (-200 + random() * 400)::int, -- Price at time of order
  (ARRAY['new', 'new', 'new', 'used'])[1 + (random() * 3)::int] -- 75% new
FROM item_orders io
JOIN items i ON io.item_id = i.id
CROSS JOIN LATERAL (
  SELECT seller_id FROM item_sellers WHERE item_id = io.item_id ORDER BY random() LIMIT 1
) s;

-- Generate reviews with realistic variability (some users review a lot, others don't)
WITH user_review_behavior AS (
  SELECT 
    id as user_id,
    CASE 
      WHEN random() < 0.10 THEN 0.9  -- 10% prolific reviewers: review 90% of purchases
      WHEN random() < 0.25 THEN 0.7  -- 15% frequent reviewers: 70% of purchases
      WHEN random() < 0.50 THEN 0.4  -- 25% occasional reviewers: 40% of purchases
      WHEN random() < 0.75 THEN 0.2  -- 25% rare reviewers: 20% of purchases
      ELSE 0.05                      -- 25% almost never review: 5% of purchases
    END as review_probability
  FROM users
),
item_review_tendency AS (
  SELECT 
    id as item_id,
    CASE 
      WHEN random() < 0.15 THEN 1.5   -- 15% controversial items: get more reviews
      WHEN random() < 0.30 THEN 1.2   -- 15% popular items: slightly more reviews
      WHEN random() < 0.70 THEN 1.0   -- 40% normal items: standard review rate
      ELSE 0.6                        -- 30% boring items: fewer reviews
    END as review_multiplier
  FROM items
)
INSERT INTO reviews (item_id, user_id, rating, title, content, verified_purchase, helpful_votes, created_at)
SELECT DISTINCT
  oi.item_id,
  o.user_id,
  -- Rating distribution varies by item type
  CASE 
    WHEN random() < 0.05 THEN 1  -- 5% terrible (1 star)
    WHEN random() < 0.15 THEN 2  -- 10% poor (2 stars)
    WHEN random() < 0.30 THEN 3  -- 15% okay (3 stars)
    WHEN random() < 0.60 THEN 4  -- 30% good (4 stars)
    ELSE 5                       -- 40% excellent (5 stars)
  END,
  (ARRAY['Great product!', 'Love it!', 'Excellent quality', 'Good value', 'Disappointed', 'Amazing!', 'Not bad', 'Perfect', 'Could be better', 'Fantastic', 'Terrible', 'Waste of money', 'Outstanding', 'Mediocre', 'Impressive'])[1 + (random() * 14)::int],
  CASE 
    WHEN random() > 0.7 THEN (ARRAY[
      'This product exceeded my expectations. Highly recommended!', 
      'Good quality for the price. Fast shipping.',
      'Exactly what I was looking for. Very satisfied.',
      'Poor quality, not worth the money.',
      'Amazing product, will buy again!',
      'Broke after a week. Very disappointed.',
      'Perfect for my needs. Great value.',
      'Overpriced for what you get.',
      'Solid build quality. Would recommend.',
      'Not as described. Returning.'
    ])[1 + (random() * 9)::int]
    ELSE NULL
  END,
  true, -- Verified purchase
  CASE 
    WHEN random() < 0.10 THEN (random() * 100)::int  -- 10% get lots of helpful votes
    WHEN random() < 0.30 THEN (random() * 25)::int   -- 20% get some helpful votes
    ELSE (random() * 5)::int                         -- 70% get few helpful votes
  END,
  o.created_at + interval '1 day' + (random() * interval '30 days')
FROM orders o
JOIN order_items oi ON o.id = oi.order_id
JOIN user_review_behavior urb ON o.user_id = urb.user_id
JOIN item_review_tendency irt ON oi.item_id = irt.item_id
WHERE random() < (urb.review_probability * irt.review_multiplier) -- Combined probability
ON CONFLICT (item_id, user_id) DO NOTHING; -- Skip duplicates

-- Update item ratings based on reviews
UPDATE items SET 
  avg_rating = COALESCE(review_stats.avg_rating, 0),
  review_count = COALESCE(review_stats.review_count, 0)
FROM (
  SELECT 
    item_id,
    ROUND(AVG(rating::decimal), 2) as avg_rating,
    COUNT(*) as review_count
  FROM reviews 
  GROUP BY item_id
) review_stats
WHERE items.id = review_stats.item_id;

-- Generate invoices for orders
INSERT INTO invoices (
  order_id, invoice_number, subtotal_cents, tax_cents, shipping_cents, 
  total_cents, status, payment_method, transaction_id, created_at, paid_at, due_date
)
SELECT 
  o.id,
  'INV-' || to_char(o.created_at, 'YYYY') || '-' || lpad(o.id::text, 8, '0'),
  o.subtotal_cents,
  o.tax_cents,
  o.shipping_cents,
  o.total_cents,
  CASE 
    WHEN o.status IN ('delivered') THEN 'paid'
    WHEN o.status IN ('shipped', 'processing') THEN 'paid'
    WHEN random() > 0.1 THEN 'paid'
    ELSE 'pending'
  END,
  (ARRAY['credit_card', 'debit_card', 'paypal', 'apple_pay'])[1 + (random() * 3)::int],
  'txn_' || md5(random()::text),
  o.created_at,
  CASE WHEN random() > 0.1 THEN o.created_at + interval '1 hour' ELSE NULL END,
  o.created_at + interval '30 days'
FROM orders o
WHERE random() > 0.05; -- 95% of orders have invoices

-- Generate some sample queries for testing (as comments)
-- Top selling products:
-- SELECT i.title, i.avg_rating, SUM(oi.quantity) as total_sold 
-- FROM items i 
-- JOIN order_items oi ON i.id = oi.item_id 
-- GROUP BY i.id, i.title, i.avg_rating 
-- ORDER BY total_sold DESC LIMIT 20;

-- Revenue by category:
-- SELECT c.name, SUM(oi.quantity * oi.unit_price_cents) as revenue_cents
-- FROM categories c
-- JOIN items i ON c.id = i.category_id
-- JOIN order_items oi ON i.id = oi.item_id
-- GROUP BY c.id, c.name
-- ORDER BY revenue_cents DESC;

-- Top customers:
-- SELECT u.first_name || ' ' || u.last_name as customer, COUNT(o.id) as order_count, SUM(o.total_cents) as total_spent
-- FROM users u
-- JOIN orders o ON u.id = o.user_id
-- GROUP BY u.id, u.first_name, u.last_name
-- ORDER BY total_spent DESC LIMIT 10;

ANALYZE; -- Update table statistics for better query planning

