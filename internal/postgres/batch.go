package postgres

import (
	"context"
	"uuid"
)

// maxBatchSize limits the number of writes in one transaction.
const maxBatchSize = 1000

// queue holds the writes to one wallet that wait for a batch.
// One worker goroutine for each queue applies them. While a batch commits, the next writes collect in the queue,
// so the batch grows with the load. All the writes of one batch share one commit and one WAL flush.
type queue struct {
	writes []*queuedWrite
}

type queuedWrite struct {
	op operation
	// done has a buffer of one, so the worker never waits for a caller.
	done chan result
	// cancelled tells the worker to skip the write, caller sets it when its context ends.
	cancelled bool
}

// write puts op into the queue of the wallet and waits for its result.
// If ctx ends while op waits in the queue, op does not apply.
// If op is already in a batch, it applies no matter what.
func (s *Store) write(ctx context.Context, id uuid.UUID, op operation) (int64, error) {
	write := &queuedWrite{op: op, done: make(chan result, 1)}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, errClosed
	}
	q, ok := s.queues[id]
	if !ok {
		q = &queue{}
		s.queues[id] = q
		s.workers.Add(1)
		go s.drain(id, q)
	}
	q.writes = append(q.writes, write)
	s.mu.Unlock()

	select {
	case r := <-write.done:
		return r.balance, r.err
	case <-ctx.Done():
		s.mu.Lock()
		write.cancelled = true
		s.mu.Unlock()
		return 0, ctx.Err()
	}
}

// drain applies the batches of the queue until the queue is empty. Then it removes the queue.
func (s *Store) drain(id uuid.UUID, q *queue) {
	defer s.workers.Done()

	for {
		batch := s.take(id, q)
		if batch == nil {
			return
		}

		ops := make([]operation, len(batch))
		for i, write := range batch {
			ops[i] = write.op
		}

		// The batch serves many callers, so it uses its own context.
		results, err := s.applyBatch(context.Background(), id, ops)
		for i, w := range batch {
			if err != nil {
				w.done <- result{err: err}
				continue
			}
			w.done <- results[i]
		}
	}
}

// take removes up to maxBatchSize writes from the queue and skips the cancelled ones.
// If no write remains, take removes the queue from the map and returns nil.
func (s *Store) take(id uuid.UUID, q *queue) []*queuedWrite {
	s.mu.Lock()
	defer s.mu.Unlock()

	var batch []*queuedWrite
	n := 0
	for n < len(q.writes) && len(batch) < maxBatchSize {
		if write := q.writes[n]; !write.cancelled {
			batch = append(batch, write)
		}
		n++
	}
	clear(q.writes[:n])
	q.writes = q.writes[n:]

	if len(batch) == 0 {
		delete(s.queues, id)
		return nil
	}
	return batch
}
