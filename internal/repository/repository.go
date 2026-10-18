// Package repository defines how players are stored, independent of the storage technology.
package repository

import "footballers/internal/domain"

// PlayerRepository is the storage contract. The service layer depends on this
// interface, not on a concrete type, so storage can be swapped (map, file, DB)
// or replaced with a fake in tests.
//
// Methods return domain.Player by value: callers get a copy and cannot corrupt
// the stored data by mutating it.
type PlayerRepository interface {
	// Add stores a new player, assigns its ID and returns the stored copy.
	// Returns domain.ErrDuplicatePlayer if a player with the same name and nation exists.
	Add(p domain.Player) (domain.Player, error)
	// Get returns the player by ID or domain.ErrPlayerNotFound.
	Get(id int) (domain.Player, error)
	// Update replaces the stored player (matched by ID) or returns domain.ErrPlayerNotFound.
	Update(p domain.Player) error
	// Delete removes the player or returns domain.ErrPlayerNotFound.
	Delete(id int) error
	// List returns all players sorted by ID.
	List() []domain.Player
	// ListByNation returns players of a nation (case-insensitive), sorted by ID.
	ListByNation(nation string) []domain.Player
	// ListByPosition returns players of a position, sorted by ID.
	ListByPosition(pos domain.Position) []domain.Player
}
