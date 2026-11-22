package service

import "footballers/internal/domain"

// The methods below are the use cases of the asynchronous evaluation
// lifecycle (Pending -> Queued -> Processing -> Completed/Failed).
// Each one is a single atomic state change built on apply, so the worker pool
// never has to lock anything itself.

// MarkQueued moves a Pending player to Queued. It fails with
// domain.ErrInvalidTransition if the player was already submitted, which is
// what prevents a player from being queued or processed twice.
func (s *PlayerService) MarkQueued(id int) (domain.Player, error) {
	return s.apply(id, "enqueue", (*domain.Player).Enqueue)
}

// UnmarkQueued undoes MarkQueued (Queued -> Pending). It is used when the
// queue rejects the player, so a rejected player is left in a valid state.
func (s *PlayerService) UnmarkQueued(id int) (domain.Player, error) {
	return s.apply(id, "dequeue", (*domain.Player).Dequeue)
}

// StartProcessing atomically "claims" a queued player (Queued -> Processing)
// and returns a snapshot to evaluate. Only one caller can win the claim.
func (s *PlayerService) StartProcessing(id int) (domain.Player, error) {
	return s.apply(id, "start processing", (*domain.Player).StartProcessing)
}

// CompleteProcessing stores the report in the player and marks it Completed.
func (s *PlayerService) CompleteProcessing(id int, r domain.Report) (domain.Player, error) {
	return s.apply(id, "complete processing of", func(p *domain.Player) error {
		return p.CompleteProcessing(r)
	})
}

// FailProcessing marks the player Failed and records the reason.
func (s *PlayerService) FailProcessing(id int, reason string) (domain.Player, error) {
	return s.apply(id, "fail processing of", func(p *domain.Player) error {
		return p.FailProcessing(reason)
	})
}
