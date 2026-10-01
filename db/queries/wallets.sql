-- name: Deposit :one
UPDATE wallets SET balance = balance + @amount WHERE id = @id RETURNING balance;

-- name: Withdraw :one
-- The WHERE clause keeps the balance at zero or above. A missing row means an unknown wallet or insufficient funds.
UPDATE wallets SET balance = balance - @amount WHERE id = @id AND balance >= @amount RETURNING balance;

-- name: WalletExists :one
SELECT EXISTS (SELECT 1 FROM wallets WHERE id = @id);

-- name: GetBalance :one
SELECT balance FROM wallets WHERE id = @id;
