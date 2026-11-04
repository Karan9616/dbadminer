package main

func seedSQL(driver string) []string {
	switch driver {
	case "pgx":
		return postgresSeed
	default:
		return mysqlSeed
	}
}

var postgresSeed = []string{
	`DROP VIEW IF EXISTS customer_order_totals`,
	`DROP PROCEDURE IF EXISTS bump_loyalty`,
	`DROP FUNCTION IF EXISTS customer_label`,
	`DROP TABLE IF EXISTS order_items`,
	`DROP TABLE IF EXISTS orders`,
	`DROP TABLE IF EXISTS products`,
	`DROP TABLE IF EXISTS sample_docs`,
	`DROP TABLE IF EXISTS customers`,
	`CREATE TABLE customers (
		id BIGSERIAL PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		phone TEXT,
		company TEXT,
		job_title TEXT,
		city TEXT,
		country TEXT,
		postal_code TEXT,
		status TEXT NOT NULL DEFAULT 'active',
		loyalty_points INTEGER NOT NULL DEFAULT 0,
		newsletter BOOLEAN NOT NULL DEFAULT FALSE,
		notes TEXT,
		external_uuid UUID NOT NULL DEFAULT gen_random_uuid(),
		website TEXT,
		referral_code TEXT,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		last_login_at TIMESTAMPTZ
	)`,
	`CREATE TABLE products (
		id BIGSERIAL PRIMARY KEY,
		sku TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		price_cents INTEGER NOT NULL CHECK (price_cents >= 0),
		active BOOLEAN NOT NULL DEFAULT TRUE
	)`,
	`CREATE TABLE orders (
		id BIGSERIAL PRIMARY KEY,
		customer_id BIGINT NOT NULL REFERENCES customers(id),
		status TEXT NOT NULL DEFAULT 'pending',
		placed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE TABLE order_items (
		id BIGSERIAL PRIMARY KEY,
		order_id BIGINT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
		product_id BIGINT NOT NULL REFERENCES products(id),
		quantity INTEGER NOT NULL CHECK (quantity > 0),
		unit_price_cents INTEGER NOT NULL CHECK (unit_price_cents >= 0)
	)`,
	`CREATE TABLE sample_docs (
		id BIGSERIAL PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT NOT NULL,
		meta JSONB NOT NULL,
		tags TEXT[] NOT NULL,
		payload BYTEA NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`,
	`CREATE INDEX orders_customer_id_idx ON orders (customer_id)`,
	`CREATE INDEX order_items_order_id_idx ON order_items (order_id)`,
	`CREATE VIEW customer_order_totals AS
		SELECT c.id AS customer_id, c.email, COUNT(o.id) AS order_count
		FROM customers c
		LEFT JOIN orders o ON o.customer_id = c.id
		GROUP BY c.id, c.email`,
	`INSERT INTO customers (
		email, name, phone, company, job_title, city, country, postal_code,
		status, loyalty_points, newsletter, notes, website, referral_code, last_login_at
	)
	SELECT
		'user' || g || '@example.com',
		'Customer ' || g,
		'+1 555 ' || lpad((1000 + g)::text, 4, '0'),
		'Acme ' || ((g % 40) + 1),
		(ARRAY['Engineer', 'Designer', 'Analyst', 'Manager', 'Founder'])[1 + (g % 5)],
		(ARRAY['London', 'Berlin', 'Tokyo', 'Austin', 'Toronto'])[1 + (g % 5)],
		(ARRAY['UK', 'DE', 'JP', 'US', 'CA'])[1 + (g % 5)],
		lpad((10000 + g)::text, 5, '0'),
		(ARRAY['active', 'active', 'active', 'paused', 'churned'])[1 + (g % 5)],
		(g * 7) % 5000,
		(g % 3) = 0,
		CASE WHEN g % 11 = 0 THEN 'VIP account' ELSE NULL END,
		CASE WHEN g % 4 = 0 THEN 'https://customer' || g || '.example' ELSE NULL END,
		'REF-' || lpad(g::text, 4, '0'),
		CASE WHEN g % 5 = 0 THEN NULL ELSE NOW() - ((g % 72) || ' hours')::interval END
	FROM generate_series(1, 1000) AS g`,
	`INSERT INTO products (sku, name, price_cents, active) VALUES
		('TEA-001', 'Earl Grey', 450, TRUE),
		('TEA-002', 'Sencha', 520, TRUE),
		('MUG-001', 'Stoneware Mug', 1800, TRUE),
		('FLT-001', 'Paper Filters', 300, FALSE)`,
	`INSERT INTO orders (customer_id, status) VALUES
		(1, 'paid'),
		(1, 'pending'),
		(2, 'paid')`,
	`INSERT INTO order_items (order_id, product_id, quantity, unit_price_cents) VALUES
		(1, 1, 2, 450),
		(1, 3, 1, 1800),
		(2, 2, 1, 520),
		(3, 1, 3, 450)`,
	`INSERT INTO sample_docs (title, body, meta, tags, payload) VALUES
		(
			'Welcome guide',
			repeat('DBAdminer demo paragraph. Long text for scrolling and truncation checks. ', 80),
			'{"kind":"guide","version":1,"flags":{"published":true,"featured":false},"scores":[4.5,4.8]}'::jsonb,
			ARRAY['docs', 'welcome', 'demo'],
			decode('89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000a49444154789c63000100000500010d0a2db40000000049454e44ae426082','hex')
		),
		(
			'Release notes',
			repeat(E'Line of release notes with enough content to exercise wide cells and copy.\n', 120),
			'{"kind":"notes","version":2,"authors":["ada","linus"],"changelog":[{"id":1,"text":"JSON cells"},{"id":2,"text":"array chips"}]}'::jsonb,
			ARRAY['release', 'notes'],
			decode(repeat('deadbeef', 512), 'hex')
		),
		(
			'Empty-ish payload',
			'Short body with rich metadata and an empty tag list for edge cases.',
			'{"kind":"edge","nested":{"a":{"b":[1,2,3]}},"empty":{}}'::jsonb,
			ARRAY[]::text[],
			decode('00', 'hex')
		)`,
	`CREATE OR REPLACE FUNCTION customer_label(p_id BIGINT)
	RETURNS TEXT
	LANGUAGE sql
	STABLE
	AS $$
		SELECT email || ' (' || name || ')'
		FROM customers
		WHERE id = p_id
	$$`,
	`CREATE OR REPLACE PROCEDURE bump_loyalty(p_id BIGINT, p_delta INTEGER)
	LANGUAGE plpgsql
	AS $$
	BEGIN
		UPDATE customers
		SET loyalty_points = loyalty_points + p_delta,
		    updated_at = NOW()
		WHERE id = p_id;
	END;
	$$`,
}

var mysqlSeed = []string{
	`SET FOREIGN_KEY_CHECKS=0`,
	`DROP VIEW IF EXISTS customer_order_totals`,
	`DROP PROCEDURE IF EXISTS bump_loyalty`,
	`DROP FUNCTION IF EXISTS customer_label`,
	`DROP TABLE IF EXISTS order_items`,
	`DROP TABLE IF EXISTS orders`,
	`DROP TABLE IF EXISTS products`,
	`DROP TABLE IF EXISTS sample_docs`,
	`DROP TABLE IF EXISTS customers`,
	`SET FOREIGN_KEY_CHECKS=1`,
	`CREATE TABLE customers (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
		email VARCHAR(255) NOT NULL,
		name VARCHAR(255) NOT NULL,
		phone VARCHAR(64) NULL,
		company VARCHAR(255) NULL,
		job_title VARCHAR(255) NULL,
		city VARCHAR(128) NULL,
		country VARCHAR(64) NULL,
		postal_code VARCHAR(32) NULL,
		status VARCHAR(32) NOT NULL DEFAULT 'active',
		loyalty_points INT NOT NULL DEFAULT 0,
		newsletter BOOLEAN NOT NULL DEFAULT FALSE,
		notes TEXT NULL,
		external_uuid CHAR(36) NOT NULL,
		website VARCHAR(255) NULL,
		referral_code VARCHAR(64) NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		last_login_at TIMESTAMP NULL,
		UNIQUE KEY customers_email_uidx (email),
		UNIQUE KEY customers_external_uuid_uidx (external_uuid)
	)`,
	`CREATE TABLE products (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
		sku VARCHAR(64) NOT NULL,
		name VARCHAR(255) NOT NULL,
		price_cents INT NOT NULL,
		active BOOLEAN NOT NULL DEFAULT TRUE,
		UNIQUE KEY products_sku_uidx (sku)
	)`,
	`CREATE TABLE orders (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
		customer_id BIGINT NOT NULL,
		status VARCHAR(32) NOT NULL DEFAULT 'pending',
		placed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		CONSTRAINT orders_customer_id_fkey
			FOREIGN KEY (customer_id) REFERENCES customers(id)
	)`,
	`CREATE TABLE order_items (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
		order_id BIGINT NOT NULL,
		product_id BIGINT NOT NULL,
		quantity INT NOT NULL,
		unit_price_cents INT NOT NULL,
		CONSTRAINT order_items_order_id_fkey
			FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE,
		CONSTRAINT order_items_product_id_fkey
			FOREIGN KEY (product_id) REFERENCES products(id)
	)`,
	`CREATE TABLE sample_docs (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		body MEDIUMTEXT NOT NULL,
		meta JSON NOT NULL,
		tags JSON NOT NULL,
		payload BLOB NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`,
	`CREATE INDEX orders_customer_id_idx ON orders (customer_id)`,
	`CREATE INDEX order_items_order_id_idx ON order_items (order_id)`,
	`CREATE VIEW customer_order_totals AS
		SELECT c.id AS customer_id, c.email, COUNT(o.id) AS order_count
		FROM customers c
		LEFT JOIN orders o ON o.customer_id = c.id
		GROUP BY c.id, c.email`,
	`INSERT INTO customers (
		email, name, phone, company, job_title, city, country, postal_code,
		status, loyalty_points, newsletter, notes, external_uuid, website, referral_code, last_login_at
	)
	WITH RECURSIVE seq AS (
		SELECT 1 AS n
		UNION ALL
		SELECT n + 1 FROM seq WHERE n < 1000
	)
	SELECT
		CONCAT('user', n, '@example.com'),
		CONCAT('Customer ', n),
		CONCAT('+1 555 ', LPAD(1000 + n, 4, '0')),
		CONCAT('Acme ', (n % 40) + 1),
		ELT(1 + (n % 5), 'Engineer', 'Designer', 'Analyst', 'Manager', 'Founder'),
		ELT(1 + (n % 5), 'London', 'Berlin', 'Tokyo', 'Austin', 'Toronto'),
		ELT(1 + (n % 5), 'UK', 'DE', 'JP', 'US', 'CA'),
		LPAD(10000 + n, 5, '0'),
		ELT(1 + (n % 5), 'active', 'active', 'active', 'paused', 'churned'),
		(n * 7) % 5000,
		(n % 3) = 0,
		IF(n % 11 = 0, 'VIP account', NULL),
		UUID(),
		IF(n % 4 = 0, CONCAT('https://customer', n, '.example'), NULL),
		CONCAT('REF-', LPAD(n, 4, '0')),
		IF(n % 5 = 0, NULL, TIMESTAMPADD(HOUR, -(n % 72), CURRENT_TIMESTAMP))
	FROM seq`,
	`INSERT INTO products (sku, name, price_cents, active) VALUES
		('TEA-001', 'Earl Grey', 450, TRUE),
		('TEA-002', 'Sencha', 520, TRUE),
		('MUG-001', 'Stoneware Mug', 1800, TRUE),
		('FLT-001', 'Paper Filters', 300, FALSE)`,
	`INSERT INTO orders (customer_id, status) VALUES
		(1, 'paid'),
		(1, 'pending'),
		(2, 'paid')`,
	`INSERT INTO order_items (order_id, product_id, quantity, unit_price_cents) VALUES
		(1, 1, 2, 450),
		(1, 3, 1, 1800),
		(2, 2, 1, 520),
		(3, 1, 3, 450)`,
	`INSERT INTO sample_docs (title, body, meta, tags, payload) VALUES
		(
			'Welcome guide',
			REPEAT('DBAdminer demo paragraph. Long text for scrolling and truncation checks. ', 80),
			CAST('{"kind":"guide","version":1,"flags":{"published":true,"featured":false},"scores":[4.5,4.8]}' AS JSON),
			JSON_ARRAY('docs', 'welcome', 'demo'),
			UNHEX('89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000a49444154789c63000100000500010d0a2db40000000049454e44ae426082')
		),
		(
			'Release notes',
			REPEAT(CONCAT('Line of release notes with enough content to exercise wide cells and copy.', CHAR(10)), 120),
			CAST('{"kind":"notes","version":2,"authors":["ada","linus"],"changelog":[{"id":1,"text":"JSON cells"},{"id":2,"text":"array chips"}]}' AS JSON),
			JSON_ARRAY('release', 'notes'),
			UNHEX(REPEAT('deadbeef', 512))
		),
		(
			'Empty-ish payload',
			'Short body with rich metadata and an empty tag list for edge cases.',
			CAST('{"kind":"edge","nested":{"a":{"b":[1,2,3]}},"empty":{}}' AS JSON),
			JSON_ARRAY(),
			UNHEX('00')
		)`,
	`CREATE FUNCTION customer_label(p_id BIGINT)
	RETURNS VARCHAR(512)
	DETERMINISTIC
	READS SQL DATA
	BEGIN
		DECLARE v_label VARCHAR(512);
		SELECT CONCAT(email, ' (', name, ')') INTO v_label
		FROM customers
		WHERE id = p_id;
		RETURN v_label;
	END`,
	`CREATE PROCEDURE bump_loyalty(IN p_id BIGINT, IN p_delta INT)
	BEGIN
		UPDATE customers
		SET loyalty_points = loyalty_points + p_delta
		WHERE id = p_id;
	END`,
}
