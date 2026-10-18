// Package service contains the use cases: it orchestrates domain rules and storage.
package service

import (
	"fmt"

	"footballers/internal/domain"
	"footballers/internal/repository"
)

// PlayerService depends on the repository INTERFACE, injected through the
// constructor. It does not know (or care) that the data lives in a map.
type PlayerService struct {
	repo repository.PlayerRepository
}

// NewPlayerService wires the service to a storage implementation.
func NewPlayerService(repo repository.PlayerRepository) *PlayerService {
	return &PlayerService{repo: repo}
}

// Register validates and stores a new player.
func (s *PlayerService) Register(name, nation string, pos domain.Position, club string) (domain.Player, error) {
	p, err := domain.NewPlayer(name, nation, pos, club)
	if err != nil {
		return domain.Player{}, fmt.Errorf("register player: %w", err)
	}
	stored, err := s.repo.Add(p)
	if err != nil {
		return domain.Player{}, fmt.Errorf("register player: %w", err)
	}
	return stored, nil
}

// Get returns a player by ID.
func (s *PlayerService) Get(id int) (domain.Player, error) {
	p, err := s.repo.Get(id)
	if err != nil {
		return domain.Player{}, fmt.Errorf("get player %d: %w", id, err)
	}
	return p, nil
}

func (s *PlayerService) Injure(id int) (domain.Player, error) {
	return s.apply(id, "injure", (*domain.Player).Injure)
}

func (s *PlayerService) Recover(id int) (domain.Player, error) {
	return s.apply(id, "recover", (*domain.Player).Recover)
}

func (s *PlayerService) Suspend(id int) (domain.Player, error) {
	return s.apply(id, "suspend", (*domain.Player).Suspend)
}

func (s *PlayerService) EndSuspension(id int) (domain.Player, error) {
	return s.apply(id, "end suspension of", (*domain.Player).EndSuspension)
}

func (s *PlayerService) Loan(id int, club string) (domain.Player, error) {
	return s.apply(id, "loan", func(p *domain.Player) error { return p.Loan(club) })
}

func (s *PlayerService) ReturnFromLoan(id int) (domain.Player, error) {
	return s.apply(id, "return from loan", (*domain.Player).ReturnFromLoan)
}

func (s *PlayerService) Release(id int) (domain.Player, error) {
	return s.apply(id, "release", (*domain.Player).Release)
}

func (s *PlayerService) Retire(id int) (domain.Player, error) {
	return s.apply(id, "retire", (*domain.Player).Retire)
}

// FindByNation returns players of the given nation.
func (s *PlayerService) FindByNation(nation string) []domain.Player {
	return s.repo.ListByNation(nation)
}

// FindByPosition returns players of the given position.
func (s *PlayerService) FindByPosition(pos domain.Position) []domain.Player {
	return s.repo.ListByPosition(pos)
}

// apply is the common pattern for every state change:
// repo.Get -> domain method -> repo.Update.
// The player is a local copy, so if the domain rule fails nothing is saved.
// Errors are wrapped with %w: context is added, but errors.Is still finds the sentinel.
func (s *PlayerService) apply(id int, action string, change func(*domain.Player) error) (domain.Player, error) {
	p, err := s.repo.Get(id)
	if err != nil {
		return domain.Player{}, fmt.Errorf("%s player %d: %w", action, id, err)
	}
	if err := change(&p); err != nil {
		return domain.Player{}, fmt.Errorf("%s player %d: %w", action, id, err)
	}
	if err := s.repo.Update(p); err != nil {
		return domain.Player{}, fmt.Errorf("%s player %d: %w", action, id, err)
	}
	return p, nil
}
