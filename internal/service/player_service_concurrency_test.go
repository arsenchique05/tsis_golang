package service

import (
	"fmt"
	"sync"
	"testing"

	"footballers/internal/domain"
	"footballers/internal/repository"
)

// Two goroutines change DIFFERENT fields of the SAME player at the same time
// (one loans him out, the other injures him). Both changes must survive.
//
// This is the "lost update" test. The repository lock alone is not enough: if
// apply did Get -> change -> Update without the service mutex, both goroutines
// could Get the same old copy, and the later Update would silently overwrite
// the earlier one. -race would NOT report that (every single call is locked);
// only this assertion on the final state catches it.
func TestConcurrentTransitionsOnSamePlayerLoseNoUpdate(t *testing.T) {
	const n = 200
	svc := NewPlayerService(repository.NewMemoryRepository())

	ids := make([]int, n)
	for i := range ids {
		p, err := svc.Register(fmt.Sprintf("Player-%d", i), "Land", domain.MID, "Home FC")
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = p.ID
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, id := range ids {
		id := id
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if _, err := svc.Loan(id, "Roma"); err != nil {
				t.Errorf("Loan(%d): %v", id, err)
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			if _, err := svc.Injure(id); err != nil {
				t.Errorf("Injure(%d): %v", id, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	for _, id := range ids {
		p, err := svc.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if p.Contract != domain.OnLoan || p.Availability != domain.Injured {
			t.Errorf("player %d: Contract=%v Availability=%v, want OnLoan+Injured (an update was lost)",
				id, p.Contract, p.Availability)
		}
	}
}

// Of N concurrent attempts to queue the same player exactly one may succeed:
// this is what stops a player from being processed twice.
func TestConcurrentMarkQueuedOnlyOneWins(t *testing.T) {
	const n = 50
	svc := NewPlayerService(repository.NewMemoryRepository())
	p, err := svc.Register("Salah", "Egypt", domain.FWD, "Liverpool")
	if err != nil {
		t.Fatal(err)
	}

	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = svc.MarkQueued(p.ID)
		}()
	}
	close(start)
	wg.Wait()

	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("%d goroutines queued the same player, want exactly 1", wins)
	}
	if got, _ := svc.Get(p.ID); got.Processing != domain.Queued {
		t.Errorf("Processing = %v, want Queued", got.Processing)
	}
}

// The service use cases for the processing lifecycle, single-threaded.
func TestServiceProcessingLifecycle(t *testing.T) {
	svc := NewPlayerService(repository.NewMemoryRepository())
	p, _ := svc.Register("Salah", "Egypt", domain.FWD, "Liverpool")
	rep, _ := domain.Evaluate(p)

	if got, err := svc.MarkQueued(p.ID); err != nil || got.Processing != domain.Queued {
		t.Fatalf("MarkQueued: %+v, %v", got, err)
	}
	if got, err := svc.UnmarkQueued(p.ID); err != nil || got.Processing != domain.Pending {
		t.Fatalf("UnmarkQueued: %+v, %v", got, err)
	}
	if _, err := svc.MarkQueued(p.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.StartProcessing(p.ID); err != nil || got.Processing != domain.Processing {
		t.Fatalf("StartProcessing: %+v, %v", got, err)
	}
	got, err := svc.CompleteProcessing(p.ID, rep)
	if err != nil || got.Processing != domain.Completed || got.Report != rep {
		t.Fatalf("CompleteProcessing: %+v, %v", got, err)
	}
	if _, err := svc.FailProcessing(p.ID, "late"); err == nil {
		t.Error("FailProcessing on a Completed player must fail")
	}
}
