package repository

import (
	"errors"
	"testing"

	"footballers/internal/domain"
)

func mustPlayer(t *testing.T, name, nation string, pos domain.Position, club string) domain.Player {
	t.Helper()
	p, err := domain.NewPlayer(name, nation, pos, club)
	if err != nil {
		t.Fatalf("NewPlayer: %v", err)
	}
	return p
}

func seed(t *testing.T) *MemoryRepository {
	t.Helper()
	r := NewMemoryRepository()
	for _, p := range []domain.Player{
		mustPlayer(t, "Messi", "Argentina", domain.FWD, "Inter Miami"),
		mustPlayer(t, "Martinez", "Argentina", domain.GK, "Aston Villa"),
		mustPlayer(t, "Mbappe", "France", domain.FWD, "Real Madrid"),
		mustPlayer(t, "Kante", "France", domain.MID, "Al Ittihad"),
	} {
		if _, err := r.Add(p); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	return r
}

func TestAddAssignsSequentialIDs(t *testing.T) {
	r := NewMemoryRepository()
	a, _ := r.Add(mustPlayer(t, "A", "X", domain.GK, "C"))
	b, _ := r.Add(mustPlayer(t, "B", "X", domain.GK, "C"))
	if a.ID != 1 || b.ID != 2 {
		t.Errorf("IDs = %d, %d; want 1, 2", a.ID, b.ID)
	}
}

func TestAddDuplicate(t *testing.T) {
	r := seed(t)
	tests := []struct {
		name, nation string
		wantErr      bool
	}{
		{"Messi", "Argentina", true},
		{"messi", "ARGENTINA", true}, // case-insensitive
		{"Messi", "Spain", false},    // same name, other nation is a different player
	}
	for _, tt := range tests {
		_, err := r.Add(mustPlayer(t, tt.name, tt.nation, domain.FWD, "FC"))
		if tt.wantErr && !errors.Is(err, domain.ErrDuplicatePlayer) {
			t.Errorf("%s/%s: err = %v, want ErrDuplicatePlayer", tt.name, tt.nation, err)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("%s/%s: unexpected error %v", tt.name, tt.nation, err)
		}
	}
}

func TestGet(t *testing.T) {
	r := seed(t)
	tests := []struct {
		name    string
		id      int
		want    string
		wantErr error
	}{
		{"existing", 1, "Messi", nil},
		{"missing", 99, "", domain.ErrPlayerNotFound},
		{"zero", 0, "", domain.ErrPlayerNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := r.Get(tt.id)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && p.Name != tt.want {
				t.Errorf("Name = %q, want %q", p.Name, tt.want)
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	r := seed(t)
	p, _ := r.Get(1)
	if err := p.Injure(); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(p); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := r.Get(1)
	if got.Availability != domain.Injured {
		t.Errorf("Availability = %v, want Injured", got.Availability)
	}

	if err := r.Update(domain.Player{ID: 99}); !errors.Is(err, domain.ErrPlayerNotFound) {
		t.Errorf("err = %v, want ErrPlayerNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	r := seed(t)
	if err := r.Delete(2); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := r.Get(2); !errors.Is(err, domain.ErrPlayerNotFound) {
		t.Errorf("deleted player still found: %v", err)
	}
	if err := r.Delete(2); !errors.Is(err, domain.ErrPlayerNotFound) {
		t.Errorf("second delete err = %v, want ErrPlayerNotFound", err)
	}
	if got := len(r.List()); got != 3 {
		t.Errorf("len(List) = %d, want 3", got)
	}
}

func TestListSortedByID(t *testing.T) {
	r := seed(t)
	// Repeat: map iteration order is random, so a missing sort would show up eventually.
	for i := 0; i < 50; i++ {
		list := r.List()
		for j, p := range list {
			if p.ID != j+1 {
				t.Fatalf("iteration %d: position %d has ID %d", i, j, p.ID)
			}
		}
	}
}

func TestListByNation(t *testing.T) {
	r := seed(t)
	tests := []struct {
		nation  string
		wantIDs []int
	}{
		{"Argentina", []int{1, 2}},
		{"france", []int{3, 4}}, // case-insensitive
		{"  France ", []int{3, 4}},
		{"Brazil", []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.nation, func(t *testing.T) {
			got := r.ListByNation(tt.nation)
			if got == nil {
				t.Fatal("result must be non-nil")
			}
			assertIDs(t, got, tt.wantIDs)
		})
	}
}

func TestListByPosition(t *testing.T) {
	r := seed(t)
	assertIDs(t, r.ListByPosition(domain.FWD), []int{1, 3})
	assertIDs(t, r.ListByPosition(domain.GK), []int{2})
	assertIDs(t, r.ListByPosition(domain.DEF), []int{})
}

// Mutating a returned copy must not change what is stored.
func TestReturnedValuesAreCopies(t *testing.T) {
	r := seed(t)

	p, _ := r.Get(1)
	p.Name = "Hacked"
	again, _ := r.Get(1)
	if again.Name != "Messi" {
		t.Errorf("Get leaked internal state: Name = %q", again.Name)
	}

	list := r.List()
	list[0].Name = "Hacked"
	if again, _ := r.Get(1); again.Name != "Messi" {
		t.Errorf("List leaked internal state: Name = %q", again.Name)
	}
}

func assertIDs(t *testing.T, got []domain.Player, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d players, want %d", len(got), len(want))
	}
	for i, p := range got {
		if p.ID != want[i] {
			t.Errorf("index %d: ID = %d, want %d", i, p.ID, want[i])
		}
	}
}
