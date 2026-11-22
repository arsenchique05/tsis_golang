package domain

import "errors"

// Sentinel errors. Callers check them with errors.Is, so they keep working
// even when a layer above wraps them with extra context via %w.
var (
	// ErrPlayerNotFound is returned when a player with the given ID does not exist.
	ErrPlayerNotFound = errors.New("player not found")
	// ErrDuplicatePlayer is returned when a player with the same name and nation already exists.
	ErrDuplicatePlayer = errors.New("player already exists")
	// ErrInvalidTransition is returned when a state change is not allowed by the rules.
	ErrInvalidTransition = errors.New("invalid state transition")
	// ErrInvalidPlayer is returned when player data fails validation.
	ErrInvalidPlayer = errors.New("invalid player")
	// ErrNotEvaluable is returned when a player cannot be evaluated (for example a retired player).
	ErrNotEvaluable = errors.New("player cannot be evaluated")
)
