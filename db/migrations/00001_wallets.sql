-- +goose Up
CREATE TABLE wallets (
    id uuid PRIMARY KEY,
    -- The WITHDRAW query keeps the balance at zero or above. The CHECK catches a bug or a manual insert.
    balance bigint NOT NULL DEFAULT 0 CHECK (balance >= 0)
);

-- +goose Down
DROP TABLE wallets;
