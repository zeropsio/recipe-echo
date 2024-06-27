CREATE TABLE IF NOT EXISTS files
(
    id         SERIAL PRIMARY KEY       NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    name       TEXT                     NOT NULL,
    url        TEXT                     NOT NULL UNIQUE,
    size       BIGINT                   NOT NULL
);
