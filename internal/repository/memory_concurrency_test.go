package repository

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"footballers/internal/domain"
)

// Many goroutines add different players at once: every Add must succeed and
// every player must get a unique ID. Without the mutex this fails under -race
// (concurrent map writes) and can duplicate IDs.
func TestConcurrentAddAssignsUniqueIDs(t *testing.T) {
	const n = 100
	r := NewMemoryRepository()

	ids := make([]int, n) // each goroutine writes only its own element
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := mustPlayer(t, fmt.Sprintf("Player-%d", i), "Land", domain.MID, "FC")
			stored, err := r.Add(p)
			if err != nil {
				t.Errorf("Add %d: %v", i, err)
				return
			}
			ids[i] = stored.ID
		}()
	}
	wg.Wait()

	seen := map[int]bool{}
	for _, id := range ids {
		if id < 1 || id > n || seen[id] {
			t.Fatalf("bad or duplicate ID %d", id)
		}
		seen[id] = true
	}
	if got := len(r.List()); got != n {
		t.Errorf("len(List) = %d, want %d", got, n)
	}
}

// The duplicate check and the insert are one critical section, so when many
// goroutines add the SAME player exactly one of them wins.
func TestConcurrentAddSamePlayerOnlyOneSucceeds(t *testing.T) {
	const n = 50
	r := NewMemoryRepository()

	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = r.Add(mustPlayer(t, "Messi", "Argentina", domain.FWD, "Inter Miami"))
		}()
	}
	close(start)
	wg.Wait()

	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, domain.ErrDuplicatePlayer):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Errorf("%d Adds succeeded, want exactly 1", ok)
	}
	if got := len(r.List()); got != 1 {
		t.Errorf("len(List) = %d, want 1", got)
	}
}

// Readers (Get, List*) and writers (Update, Delete) at the same time.
// The assertion is mostly "no race and no panic" under -race, plus basic consistency.
func TestConcurrentReadsAndWrites(t *testing.T) {
	r := seed(t) // 4 players, IDs 1..4

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		w := w
		wg.Add(1)
		go func() { // writer: toggles the availability of its own player
			defer wg.Done()
			id := w + 1
			for i := 0; i < 200; i++ {
				p, err := r.Get(id)
				if err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				if p.Availability == domain.Fit {
					p.Availability = domain.Injured
				} else {
					p.Availability = domain.Fit
				}
				if err := r.Update(p); err != nil {
					t.Errorf("Update: %v", err)
					return
				}
			}
		}()
		wg.Add(1)
		go func() { // reader: lists and looks up
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if got := len(r.List()); got != 4 {
					t.Errorf("len(List) = %d, want 4", got)
					return
				}
				r.ListByNation("France")
				r.ListByPosition(domain.FWD)
				if _, err := r.Get(w + 1); err != nil {
					t.Errorf("Get: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
