-- make load-wallets runs this file with psql -v count=<number of wallets>.
-- load/wallets.js makes the same IDs. The upsert resets the balances, so each run starts from the same state.
INSERT INTO wallets (id, balance)
SELECT format('00000000-0000-4000-9000-%s', lpad(n::text, 12, '0'))::uuid, 1000000
FROM generate_series(1, :count) AS n
ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance;
