# Wallet service: specification

This document holds the context of the project.

## Context

The project is a test assignment for a Go developer position.

## Functional requirements

### Change the wallet balance

The service accepts a REST request:

```
POST api/v1/wallet
{
  valletId: UUID,
  operationType: DEPOSIT or WITHDRAW,
  amount: 1000
}
```

After the service receives the request, it changes the wallet balance in the database.

- `DEPOSIT` adds `amount` to the balance.
- `WITHDRAW` subtracts `amount` from the balance.

### Get the wallet balance

The service returns the balance of a wallet:

```
GET api/v1/wallets/{WALLET_UUID}
```

## Non-functional requirements

### Concurrency

The task asks for special attention to problems in a concurrent environment. The target load is 1000 RPS on one wallet.

### No 5xx errors

The service must process every request. No request can fail with a 50X error.

### Stack

- Go
- PostgreSQL
- Docker

### Deployment

- The application runs in a Docker container.
- The database runs in a Docker container.
- `docker-compose` starts the full system.
- A migration tool applies the database schema.

### Configuration

The application reads environment variables from the file `config.env`.

## Decisions

### 1. Field name

`valletId` is considered a typo in the task. The request uses `walletId`. A request with `valletId` gets `400` with the code `INVALID_REQUEST`.

### 2. Path shape

Both endpoints use the singular `wallet`:

- `POST /api/v1/wallet`
- `GET /api/v1/wallet/{walletId}`

One resource has one name. The plural form in the task does not agree with the write endpoint.

### 3. Leading slash

All paths start with `/`, for example `/api/v1/wallet`.

An HTTP request path always starts with `/`. The task omits it in the text only.

### 4. Operation on an unknown wallet

The service does not create wallets. `POST` for an unknown wallet returns `404` with the code `WALLET_NOT_FOUND`. No balance changes.

Implicit creation on `DEPOSIT` hides client mistakes in the wallet ID.

Consequence: Wallets get into the database outside the API. The tests insert them directly.

### 5. Unknown wallet on read

`GET` for an unknown wallet returns `404` with the code `WALLET_NOT_FOUND`.

### 6. Insufficient funds

The balance can never go below zero. `WITHDRAW` with `amount` greater than the balance returns `409` with the code `INSUFFICIENT_FUNDS`.

### 7. Amount type and range

`amount` is a JSON integer. The service stores it as `int64` in Go and `BIGINT` in PostgreSQL. The same applies to `balance`.

- Zero is valid. The operation succeeds and the balance does not change.
- A negative value gets `400` with the code `INVALID_REQUEST`.
- A fraction, for example `10.5`, gets `400` with the code `INVALID_REQUEST`.
- There is no maximum value other than the `int64` range. A number above this range gets `400` with the code `INVALID_REQUEST`.

### 8. Currency and units

A wallet has no currency. `amount` and `balance` are in one abstract undivisible unit.

### 9. Response bodies

Both endpoints return the same body on success:

```json
{
  "walletId": "5f1c8a2e-4b7d-4c1a-9f3e-2d6b8a0c7e41",
  "balance": 1500
}
```

For `POST`, `balance` is the balance after the operation.

Every error uses the Problem Details format from RFC 9457, with `Content-Type: application/problem+json`:

```json
{
  "title": "Not Found",
  "status": 404,
  "detail": "Wallet 5f1c8a2e-4b7d-4c1a-9f3e-2d6b8a0c7e41 does not exist.",
  "code": "WALLET_NOT_FOUND",
  "retryable": false
}
```

| Field       | Meaning                                                               |
| ----------- | --------------------------------------------------------------------- |
| `title`     | The HTTP reason phrase for `status`.                                  |
| `status`    | The HTTP status code.                                                 |
| `detail`    | A human-readable text for this case.                                  |
| `code`      | A stable, machine-readable error code. Decision 10 gives the list.    |
| `retryable` | `true` when a retry of the same request can succeed. See decision 12. |

The body has no `type` field. RFC 9457 then reads `type` as `about:blank`.

Outside the scope: An unknown path gets the plain-text `404` of the Go `ServeMux`. A request that the client cancels gets `500 INTERNAL_ERROR`, but the client does not receive it.

### 10. Status codes

| Case                                                                                              | Status | `code`                      | `retryable`                |
| ------------------------------------------------------------------------------------------------- | ------ | --------------------------- | -------------------------- |
| The operation is applied                                                                          | `200`  |                             |                            |
| The balance is read                                                                               | `200`  |                             |                            |
| Malformed JSON, a missing or unknown field, a bad UUID, a bad `operationType`, a bad `amount`     | `400`  | `INVALID_REQUEST`           | `false`                    |
| The wallet does not exist                                                                         | `404`  | `WALLET_NOT_FOUND`          | `false`                    |
| The HTTP method is wrong                                                                          | `405`  | `METHOD_NOT_ALLOWED`        | `false`                    |
| The balance is less than `amount` on `WITHDRAW`                                                   | `409`  | `INSUFFICIENT_FUNDS`        | `false`                    |
| The balance goes above the `BIGINT` maximum on `DEPOSIT`                                          | `409`  | `BALANCE_LIMIT_EXCEEDED`    | `false`                    |
| No database connection is free within the wait limit                                              | `429`  | `SERVICE_OVERLOADED`        | `true`                     |
| The database is not available                                                                     | `503`  | `DATABASE_UNAVAILABLE`      | `true`                     |
| The `COMMIT` of a write or the read of a `GET` reached the database, but result did not come back | `503`  | `OPERATION_OUTCOME_UNKNOWN` | `false` (`true` for `GET`) |
| An unexpected error (a bug)                                                                       | `500`  | `INTERNAL_ERROR`            | `false`                    |

The `429` response has no `Retry-After` header. The client chooses the retry delay, for example exponential backoff with jitter. `retryable: true` tells it that a retry is safe.

### 11. Meaning of "no 5xx"

Under the target load (1000 RPS on one wallet) with a healthy database, the service returns no `5xx`.

The service absorbs these failure modes:

- Row lock contention. Requests wait in the PostgreSQL row lock queue (decision 14). They get no error.
- Connection pool limit. A request waits for a free connection up to a configured limit. After the limit, it gets `429`. The pool size and the wait limit must be large enough that the target load gets no `429`.
- Deadlocks and serialization failures. The strategy in decision 14 does not cause them.

Outside the scope: A database outage or restart gives `503`. A bug gives `500`.

### 12. Idempotency

The request has no idempotency key. Every error body has the field `retryable`.

`retryable` is `true` only when the service knows that the operation did not apply, and a later retry can succeed.

If the `COMMIT` reached the database but the confirmation did not come back, the service returns `OPERATION_OUTCOME_UNKNOWN` with `retryable: false`. `GET` changes nothing, so this case is `retryable: true` for `GET`.

Known limitation: If the client loses the response (a client timeout or a network fault), a retry of `POST` can apply the operation two times. The service stores no history (decision 13), so it cannot find the duplicate.

Reason: This keeps the API simple, and it gives the client a clear signal for each error that the service sees.

### 13. History

The database stores only the current balance, not the list of change events.

### 14. Concurrency strategy

Each write is one transaction. The PostgreSQL row lock puts all writes to one wallet in sequence.

```sql
BEGIN;
SELECT balance FROM wallets WHERE id = $1 FOR UPDATE;
-- The service applies the operation to the balance in Go.
UPDATE wallets SET balance = $2 WHERE id = $1;
COMMIT;
```

If the `SELECT` finds no row, the service returns `404`.

The service applies the operation in Go, with the rules in the `wallet` package:

- `DEPOSIT` above the `BIGINT` maximum gives `409 BALANCE_LIMIT_EXCEEDED` (decision 7).
- `WITHDRAW` above the balance gives `409 INSUFFICIENT_FUNDS` (decision 6).

The `CHECK (balance >= 0)` constraint of the table stays as a safety net against a bug.

Properties:

- Each transaction locks one row, so deadlocks cannot occur.
- `SELECT ... FOR UPDATE` in `READ COMMITTED` gives no serialization failures.
- The strategy works with more than one application instance.
- An error before the `COMMIT` means that nothing applied, because the server rolls back a transaction that does not commit. Only a lost `COMMIT` result gives `OPERATION_OUTCOME_UNKNOWN` (decision 12).

Each write holds the row lock until the commit flushes the WAL. One wallet thus gets at most one commit for each WAL flush. pgbench with 50 clients and one row gave 524 commits per second on WSL2, where `fdatasync` takes 1.4 ms. The target load needs about 667 writes per second.

#### Group commit

The service groups the writes to one wallet, so that many writes share one transaction and one WAL flush:

- Each wallet with pending writes has a queue in the service and one worker goroutine.
- The worker takes all the writes from the queue, up to 1000, and applies them in one transaction, in the queue order.
- While a batch commits, the next writes collect in the queue. Thus the batch grows with the load, and at low load a write waits for no timer.
- Each write gets its own result. A failed write, for example `INSUFFICIENT_FUNDS`, does not change the balance for the next writes in the batch.
- An error of the transaction goes to every write in the batch.
- When the queue is empty, the worker removes it and stops.

Cancellation: If the context of a request ends while its write waits in the queue, the write does not apply. If the write is already in a batch, it applies. The batch does not use the context of one request, because it serves many requests.

Each instance groups only its own writes. The row lock keeps the result correct with more than one instance.

### 15. Test scope

Unit tests for request validation, the handlers, and the error mapping. The storage layer is a mock.

Integration tests with a real PostgreSQL in Docker.

A load test with k6 checks decision 11 on a local machine.

### 16. Contents of `config.env`

Proposed variables:

| Variable             | Example value | Meaning                               |
| -------------------- | ------------- | ------------------------------------- |
| `HTTP_PORT`          | `8080`        | The port of the HTTP server.          |
| `POSTGRES_HOST`      | `db`          | The database host.                    |
| `POSTGRES_PORT`      | `5432`        | The database port.                    |
| `POSTGRES_USER`      | `wallet`      | The database user.                    |
| `POSTGRES_PASSWORD`  | `wallet`      | The database password.                |
| `POSTGRES_DB`        | `wallet`      | The database name.                    |
| `DB_MAX_CONNS`       | `50`          | The size of the connection pool.      |
| `DB_ACQUIRE_TIMEOUT` | `2s`          | The wait limit for a free connection. |

### 17. Graceful shutdown, health checks, and logging

Not in the scope for now.

### 18. API documentation

Not in the scope for now.
