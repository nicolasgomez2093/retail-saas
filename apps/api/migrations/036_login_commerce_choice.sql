CREATE INDEX active_users_by_email ON users(email) WHERE status='active';

CREATE TABLE login_choices (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash)=32),
    candidates jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX login_choices_by_expiry ON login_choices(expires_at);
