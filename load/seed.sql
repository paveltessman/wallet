-- make load runs this file with psql -v id=<wallet ID>.
-- The upsert resets the balance, so each run starts from the same state.
-- The balance is larger than the WITHDRAW amounts that can wait in the row lock queue at one time.
INSERT INTO wallets (id, balance) VALUES (:'id', 1000000)
ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance;
