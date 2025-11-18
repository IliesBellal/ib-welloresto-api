CREATE TABLE merchant (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255),
    creation_date TIMESTAMP DEFAULT NOW()
);

CREATE TABLE users (
    user_id SERIAL PRIMARY KEY,
    merchant_id INT REFERENCES merchant(id) ON DELETE CASCADE,
    name VARCHAR(100) UNIQUE NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    email VARCHAR(150),
    password_hash TEXT,
    pin_code VARCHAR(20),
    enabled BOOLEAN DEFAULT TRUE,
    creation_date TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_users_merchant_id ON users(merchant_id);

CREATE TABLE auth_tokens (
    token TEXT PRIMARY KEY,
    user_id INT REFERENCES users(user_id) ON DELETE CASCADE,
    device_id VARCHAR(50),
    created_at TIMESTAMP DEFAULT NOW(),
    expires_at TIMESTAMP
);
