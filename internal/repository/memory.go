package repository

import (
	"fmt"
	"sort"
	"strings"

	"footballers/internal/domain"
)

// MemoryRepository keeps players in a map. It is NOT safe for concurrent use:
// concurrency is out of scope for this checkpoint (a sync.RWMutex would be added later).
type MemoryRepository struct {
	players map[int]domain.Player
	nextID  int
}

// Compile-time check that *MemoryRepository satisfies the interface.
var _ PlayerRepository = (*MemoryRepository)(nil)

// NewMemoryRepository creates an empty repository. IDs start at 1.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		players: make(map[int]domain.Player),
		nextID:  1,
	}
}

func (r *MemoryRepository) Add(p domain.Player) (domain.Player, error) {
	for _, existing := range r.players {
		if strings.EqualFold(existing.Name, p.Name) && strings.EqualFold(existing.Nation, p.Nation) {
			return domain.Player{}, fmt.Errorf("%w: %s (%s)", domain.ErrDuplicatePlayer, p.Name, p.Nation)
		}
	}
	p.ID = r.nextID
	r.nextID++
	r.players[p.ID] = p // p is a copy, so the caller's value is independent
	return p, nil
}

func (r *MemoryRepository) Get(id int) (domain.Player, error) {
	p, ok := r.players[id]
	if !ok {
		return domain.Player{}, fmt.Errorf("%w: id %d", domain.ErrPlayerNotFound, id)
	}
	return p, nil
}

func (r *MemoryRepository) Update(p domain.Player) error {
	if _, ok := r.players[p.ID]; !ok {
		return fmt.Errorf("%w: id %d", domain.ErrPlayerNotFound, p.ID)
	}
	r.players[p.ID] = p
	return nil
}

func (r *MemoryRepository) Delete(id int) error {
	if _, ok := r.players[id]; !ok {
		return fmt.Errorf("%w: id %d", domain.ErrPlayerNotFound, id)
	}
	delete(r.players, id)
	return nil
}

func (r *MemoryRepository) List() []domain.Player {
	return r.filter(func(domain.Player) bool { return true })
}

func (r *MemoryRepository) ListByNation(nation string) []domain.Player {
	nation = strings.TrimSpace(nation)
	return r.filter(func(p domain.Player) bool {
		return strings.EqualFold(p.Nation, nation)
	})
}

func (r *MemoryRepository) ListByPosition(pos domain.Position) []domain.Player {
	return r.filter(func(p domain.Player) bool { return p.Position == pos })
}

// filter collects matching players into a new slice and sorts it by ID.
// Go randomizes map iteration order, so sorting is required for stable output.
// make(..., 0, n) returns an empty NON-nil slice when nothing matches.
func (r *MemoryRepository) filter(match func(domain.Player) bool) []domain.Player {
	result := make([]domain.Player, 0, len(r.players))
	for _, p := range r.players {
		if match(p) {
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
