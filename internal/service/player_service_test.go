package service

import (
	"errors"
	"testing"

	"footballers/internal/domain"
	"footballers/internal/repository"
)

// fakeRepo is a hand-written test double. It works only because the service
// depends on the PlayerRepository interface, not on MemoryRepository.
type fakeRepo struct {
	players    map[int]domain.Player
	updateErr  error // if set, Update fails with this error
	updateCall int
}

var _ repository.PlayerRepository = (*fakeRepo)(nil)

func newFakeRepo(players ...domain.Player) *fakeRepo {
	f := &fakeRepo{players: map[int]domain.Player{}}
	for _, p := range players {
		f.players[p.ID] = p
	}
	return f
}

func (f *fakeRepo) Add(p domain.Player) (domain.Player, error) {
	p.ID = len(f.players) + 1
	f.players[p.ID] = p
	return p, nil
}

func (f *fakeRepo) Get(id int) (domain.Player, error) {
	p, ok := f.players[id]
	if !ok {
		return domain.Player{}, domain.ErrPlayerNotFound
	}
	return p, nil
}

func (f *fakeRepo) Update(p domain.Player) error {
	f.updateCall++
	if f.updateErr != nil {
		return f.updateErr
	}
	f.players[p.ID] = p
	return nil
}

func (f *fakeRepo) Delete(id int) error                            { delete(f.players, id); return nil }
func (f *fakeRepo) List() []domain.Player                          { return nil }
func (f *fakeRepo) ListByNation(string) []domain.Player            { return nil }
func (f *fakeRepo) ListByPosition(domain.Position) []domain.Player { return nil }

func contractedPlayer(id int) domain.Player {
	return domain.Player{ID: id, Name: "Fake", Nation: "Land", Position: domain.MID,
		Contract: domain.Contracted, Availability: domain.Fit, Club: "Home FC"}
}

func TestServiceWithFakeRepo_Injure(t *testing.T) {
	repo := newFakeRepo(contractedPlayer(1))
	svc := NewPlayerService(repo)

	got, err := svc.Injure(1)
	if err != nil {
		t.Fatalf("Injure: %v", err)
	}
	if got.Availability != domain.Injured {
		t.Errorf("returned Availability = %v, want Injured", got.Availability)
	}
	if repo.players[1].Availability != domain.Injured {
		t.Error("change was not persisted through Update")
	}
	if repo.updateCall != 1 {
		t.Errorf("Update called %d times, want 1", repo.updateCall)
	}
}

func TestServiceWithFakeRepo_InvalidTransitionDoesNotSave(t *testing.T) {
	p := contractedPlayer(1)
	p.Availability = domain.Injured
	repo := newFakeRepo(p)
	svc := NewPlayerService(repo)

	_, err := svc.Injure(1) // already injured
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("err = %v, want ErrInvalidTransition", err)
	}
	if repo.updateCall != 0 {
		t.Error("Update must not be called when the domain rule fails")
	}
}

func TestServiceWithFakeRepo_NotFound(t *testing.T) {
	svc := NewPlayerService(newFakeRepo())
	if _, err := svc.Injure(42); !errors.Is(err, domain.ErrPlayerNotFound) {
		t.Errorf("err = %v, want ErrPlayerNotFound", err)
	}
	if _, err := svc.Get(42); !errors.Is(err, domain.ErrPlayerNotFound) {
		t.Errorf("Get err = %v, want ErrPlayerNotFound", err)
	}
}

func TestServiceWithFakeRepo_StorageErrorIsWrapped(t *testing.T) {
	boom := errors.New("disk on fire")
	repo := newFakeRepo(contractedPlayer(1))
	repo.updateErr = boom
	svc := NewPlayerService(repo)

	if _, err := svc.Injure(1); !errors.Is(err, boom) {
		t.Errorf("err = %v, want it to wrap the storage error", err)
	}
}

// Integration-style tests with the real in-memory repository.

func TestServiceWithMemoryRepo_Lifecycle(t *testing.T) {
	svc := NewPlayerService(repository.NewMemoryRepository())

	p, err := svc.Register("Salah", "Egypt", domain.FWD, "Liverpool")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	steps := []struct {
		name string
		do   func() (domain.Player, error)
		want func(domain.Player) bool
	}{
		{"loan", func() (domain.Player, error) { return svc.Loan(p.ID, "Roma") },
			func(x domain.Player) bool { return x.Contract == domain.OnLoan && x.LoanClub == "Roma" }},
		{"injure", func() (domain.Player, error) { return svc.Injure(p.ID) },
			func(x domain.Player) bool { return x.Contract == domain.OnLoan && x.Availability == domain.Injured }},
		{"recover", func() (domain.Player, error) { return svc.Recover(p.ID) },
			func(x domain.Player) bool { return x.Availability == domain.Fit }},
		{"return from loan", func() (domain.Player, error) { return svc.ReturnFromLoan(p.ID) },
			func(x domain.Player) bool { return x.Contract == domain.Contracted && x.LoanClub == "" }},
		{"suspend", func() (domain.Player, error) { return svc.Suspend(p.ID) },
			func(x domain.Player) bool { return x.Availability == domain.Suspended }},
		{"end suspension", func() (domain.Player, error) { return svc.EndSuspension(p.ID) },
			func(x domain.Player) bool { return x.Availability == domain.Fit }},
		{"release", func() (domain.Player, error) { return svc.Release(p.ID) },
			func(x domain.Player) bool { return x.Contract == domain.FreeAgent && x.Club == "" }},
		{"retire", func() (domain.Player, error) { return svc.Retire(p.ID) },
			func(x domain.Player) bool { return x.Contract == domain.Retired }},
	}

	for _, s := range steps {
		got, err := s.do()
		if err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if !s.want(got) {
			t.Fatalf("%s: unexpected state %+v", s.name, got)
		}
		// The persisted state must match what was returned.
		stored, _ := svc.Get(p.ID)
		if stored != got {
			t.Fatalf("%s: stored %+v != returned %+v", s.name, stored, got)
		}
	}

	// Retired is final.
	if _, err := svc.Injure(p.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("injure retired: err = %v, want ErrInvalidTransition", err)
	}
}

func TestServiceWithMemoryRepo_Register(t *testing.T) {
	svc := NewPlayerService(repository.NewMemoryRepository())

	if _, err := svc.Register("", "Egypt", domain.FWD, "Liverpool"); !errors.Is(err, domain.ErrInvalidPlayer) {
		t.Errorf("empty name: err = %v, want ErrInvalidPlayer", err)
	}
	if _, err := svc.Register("Salah", "Egypt", domain.FWD, "Liverpool"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register("Salah", "Egypt", domain.FWD, "Roma"); !errors.Is(err, domain.ErrDuplicatePlayer) {
		t.Errorf("duplicate: err = %v, want ErrDuplicatePlayer", err)
	}
}

func TestServiceWithMemoryRepo_Find(t *testing.T) {
	svc := NewPlayerService(repository.NewMemoryRepository())
	svc.Register("Messi", "Argentina", domain.FWD, "Inter Miami")
	svc.Register("Martinez", "Argentina", domain.GK, "Aston Villa")
	svc.Register("Mbappe", "France", domain.FWD, "Real Madrid")

	if got := len(svc.FindByNation("argentina")); got != 2 {
		t.Errorf("FindByNation = %d players, want 2", got)
	}
	if got := len(svc.FindByPosition(domain.FWD)); got != 2 {
		t.Errorf("FindByPosition = %d players, want 2", got)
	}
}
