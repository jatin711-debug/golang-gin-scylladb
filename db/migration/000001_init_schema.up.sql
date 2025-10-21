CREATE TABLE IF NOT EXISTS Users (
    id UUID PRIMARY KEY,
    email TEXT,
    name TEXT,
    created_at TIMESTAMP
);
