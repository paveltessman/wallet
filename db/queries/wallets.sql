-- name: LockBalance :one
-- FOR UPDATE holds the row lock until the commit, so the writes to one wallet run in sequence.
SELECT balance FROM wallets WHERE id = @id FOR UPDATE;

-- name: SetBalance :exec
UPDATE wallets SET balance = @balance WHERE id = @id;

-- name: GetBalance :one
SELECT balance FROM wallets WHERE id = @id;
