package postgres

import "uuid"

// QueueLen returns the number of writes that wait in the queue of the wallet, the cancelled ones included.
func QueueLen(s *Store, id uuid.UUID) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if q, ok := s.queues[id]; ok {
		return len(q.writes)
	}
	return 0
}

// QueueCount returns the number of wallets that have a queue.
func QueueCount(s *Store) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.queues)
}
