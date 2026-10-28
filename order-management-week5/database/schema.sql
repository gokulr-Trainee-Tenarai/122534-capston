CREATE TABLE IF NOT EXISTS orders (
    id BIGSERIAL PRIMARY KEY,
    customer_id BIGINT NOT NULL CHECK (customer_id > 0),
    product_id BIGINT NOT NULL CHECK (product_id > 0),
    quantity INTEGER NOT NULL CHECK (quantity BETWEEN 1 AND 10000),
    unit_price NUMERIC(12,2) NOT NULL CHECK (unit_price > 0),
    total_amount NUMERIC(14,2) NOT NULL CHECK (total_amount > 0),
    status VARCHAR(16) NOT NULL CHECK (status IN ('CREATED','CONFIRMED','CANCELLED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_orders_customer_id ON orders(customer_id);
CREATE INDEX IF NOT EXISTS idx_orders_product_id ON orders(product_id);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at DESC);

CREATE TABLE IF NOT EXISTS users (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(64) NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role VARCHAR(16) NOT NULL CHECK (role IN ('ADMIN', 'CUSTOMER')),
    customer_id BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS products (
    id BIGINT PRIMARY KEY CHECK (id > 0),
    name VARCHAR(200) NOT NULL,
    price NUMERIC(12,2) NOT NULL CHECK (price > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
