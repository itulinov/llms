-- Локальные учётные записи отделены от кадровых карточек.
CREATE TABLE accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    login text NOT NULL CHECK (
        char_length(login) BETWEEN 3 AND 128
        AND login COLLATE "C" !~ '[^A-Za-z0-9._@+-]'
    ),
    password_hash text NOT NULL CHECK (length(password_hash) > 0),
    role text NOT NULL CHECK (role IN ('admin', 'employee')),
    is_active boolean NOT NULL DEFAULT true,
    collaborator_id uuid UNIQUE REFERENCES collaborators(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (role <> 'employee' OR collaborator_id IS NOT NULL)
);

CREATE UNIQUE INDEX accounts_login_lower_idx ON accounts (lower(login));

CREATE TRIGGER set_updated_at BEFORE UPDATE ON accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Исходный токен хранится только у клиента; здесь SHA-256 (32 байта).
CREATE TABLE auth_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > created_at)
);

CREATE INDEX auth_sessions_account_id_idx ON auth_sessions (account_id);
CREATE INDEX auth_sessions_expires_at_idx ON auth_sessions (expires_at);
