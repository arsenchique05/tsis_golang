package processing

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"footballers/internal/domain"
	"footballers/internal/repository"
	"footballers/internal/service"
)

// hangGuard is only a safety net so that a bug turns into a failed test
// instead of a hung test run. Tests never rely on it for synchronization:
// they wait on channels and WaitGroups.
const hangGuard = 5 * time.Second

func newSvc() *service.PlayerService {
	return service.NewPlayerService(repository.NewMemoryRepository())
}

// register creates n valid players and returns their IDs.
func register(t *testing.T, svc *service.PlayerService, n int) []int {
	t.Helper()
	ids := make([]int, 0, n)
	for i := 0; i < n; i++ {
		p, err := svc.Register(fmt.Sprintf("Player-%d", i), "Land", domain.MID, "Home FC")
		if err != nil {
			t.Fatalf("Register: %v", err)
		}
		ids = append(ids, p.ID)
	}
	return ids
}

func mustGet(t *testing.T, svc *service.PlayerService, id int) domain.Player {
	t.Helper()
	p, err := svc.Get(id)
	if err != nil {
		t.Fatalf("Get(%d): %v", id, err)
	}
	return p
}

func mustSubmit(t *testing.T, p *Pool, id int) {
	t.Helper()
	if err := p.Submit(id); err != nil {
		t.Fatalf("Submit(%d): %v", id, err)
	}
}

// gate is an evaluator that lets tests control exactly when evaluations finish.
// Every evaluation announces itself on started and then blocks until open().
type gate struct {
	started chan int // player IDs of evaluations that have begun
	release chan struct{}
	once    sync.Once

	inFlight    atomic.Int32 // evaluations running right now
	maxInFlight atomic.Int32 // highest value inFlight ever reached
}

func newGate() *gate {
	return &gate{started: make(chan int, 1000), release: make(chan struct{})}
}

func (g *gate) open() { g.once.Do(func() { close(g.release) }) }

func (g *gate) eval(p domain.Player) (domain.Report, error) {
	n := g.inFlight.Add(1)
	for {
		m := g.maxInFlight.Load()
		if n <= m || g.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	g.started <- p.ID
	<-g.release
	g.inFlight.Add(-1)
	return domain.Evaluate(p)
}

func (g *gate) waitStarted(t *testing.T) int {
	t.Helper()
	select {
	case id := <-g.started:
		return id
	case <-time.After(hangGuard):
		t.Fatal("timed out waiting for an evaluation to start")
		return 0
	}
}

// startPool builds and starts a pool. The cleanup opens the gate (so blocked
// workers can finish) and shuts the pool down even if the test failed early.
func startPool(t *testing.T, svc *service.PlayerService, workers, queue int, g *gate) *Pool {
	t.Helper()
	cfg := Config{Workers: workers, QueueSize: queue}
	if g != nil {
		cfg.Evaluate = g.eval
	}
	p, err := NewPool(svc, cfg)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if g != nil {
			g.open()
		}
		p.Shutdown()
	})
	return p
}

// 1. Startup (and configuration validation).
func TestNewPoolValidation(t *testing.T) {
	svc := newSvc()
	tests := []struct {
		name string
		svc  *service.PlayerService
		cfg  Config
	}{
		{"one worker", svc, Config{Workers: 1, QueueSize: 5}},
		{"zero workers", svc, Config{Workers: 0, QueueSize: 5}},
		{"zero queue", svc, Config{Workers: 2, QueueSize: 0}},
		{"nil service", nil, Config{Workers: 2, QueueSize: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewPool(tt.svc, tt.cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Errorf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestStartLaunchesWorkers(t *testing.T) {
	p, err := NewPool(newSvc(), Config{Workers: 3, QueueSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.ActiveWorkers(); got != 0 {
		t.Errorf("ActiveWorkers before Start = %d, want 0", got)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := p.ActiveWorkers(); got != 3 {
		t.Errorf("ActiveWorkers after Start = %d, want 3", got)
	}
	if err := p.Start(); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second Start err = %v, want ErrAlreadyStarted", err)
	}
	p.Shutdown()
}

func TestSubmitBeforeStart(t *testing.T) {
	svc := newSvc()
	id := register(t, svc, 1)[0]
	p, _ := NewPool(svc, Config{Workers: 2, QueueSize: 2})

	if err := p.Submit(id); !errors.Is(err, ErrPoolNotStarted) {
		t.Errorf("err = %v, want ErrPoolNotStarted", err)
	}
	if st := mustGet(t, svc, id).Processing; st != domain.Pending {
		t.Errorf("Processing = %v, want Pending", st)
	}
}

// 2, 3, 5, 6. Asynchronous submission, many tasks, correct results and states.
func TestSubmittedTasksAreProcessedWithCorrectResults(t *testing.T) {
	svc := newSvc()
	ids := register(t, svc, 30)
	p := startPool(t, svc, 2, len(ids), nil)

	for _, id := range ids {
		mustSubmit(t, p, id)
	}
	p.Shutdown() // waits until every accepted task is done

	seen := map[domain.Report]bool{}
	for _, id := range ids {
		got := mustGet(t, svc, id)
		if got.Processing != domain.Completed {
			t.Fatalf("player %d: Processing = %v, want Completed", id, got.Processing)
		}
		// The stored report must belong to THIS player: recompute it from the
		// player's own data. (Evaluate depends on the name, so a report written
		// to the wrong player would not match.)
		want, err := domain.Evaluate(got)
		if err != nil {
			t.Fatal(err)
		}
		if got.Report != want {
			t.Errorf("player %d: Report = %+v, want %+v", id, got.Report, want)
		}
		seen[got.Report] = true
	}
	if len(seen) < 10 {
		t.Errorf("only %d distinct reports for %d players; results look mixed up", len(seen), len(ids))
	}
}

// Submit must return without waiting for the evaluation.
func TestSubmitDoesNotWaitForProcessing(t *testing.T) {
	g := newGate()
	svc := newSvc()
	id := register(t, svc, 1)[0]
	p := startPool(t, svc, 2, 2, g)

	mustSubmit(t, p, id) // would deadlock here if Submit waited: the gate is closed
	g.waitStarted(t)

	if st := mustGet(t, svc, id).Processing; st != domain.Processing {
		t.Errorf("Processing = %v, want Processing (evaluation still blocked)", st)
	}
	// Reading while the task is being processed works too (requirement 10).
	g.open()
	p.Shutdown()
	if st := mustGet(t, svc, id).Processing; st != domain.Completed {
		t.Errorf("Processing = %v, want Completed", st)
	}
}

// 4. Evidence that tasks really overlap, and that concurrency is bounded.
func TestTasksAreProcessedConcurrentlyWithinWorkerLimit(t *testing.T) {
	const workers = 2
	g := newGate()
	svc := newSvc()
	ids := register(t, svc, 4)
	p := startPool(t, svc, workers, len(ids), g)

	for _, id := range ids {
		mustSubmit(t, p, id)
	}

	// Both workers must be inside an evaluation at the same time. Neither can
	// finish (the gate is closed), so this can only happen if they overlap.
	a, b := g.waitStarted(t), g.waitStarted(t)
	if a == b {
		t.Fatalf("both workers took player %d", a)
	}
	for _, id := range []int{a, b} {
		if st := mustGet(t, svc, id).Processing; st != domain.Processing {
			t.Errorf("player %d: Processing = %v, want Processing", id, st)
		}
	}
	if got := g.inFlight.Load(); got != workers {
		t.Errorf("in flight = %d, want %d", got, workers)
	}

	g.open()
	p.Shutdown()

	if got := g.maxInFlight.Load(); got != workers {
		t.Errorf("max concurrent evaluations = %d, want exactly %d (the worker count)", got, workers)
	}
	for _, id := range ids {
		if st := mustGet(t, svc, id).Processing; st != domain.Completed {
			t.Errorf("player %d: Processing = %v, want Completed", id, st)
		}
	}
}

// 7. Invalid and duplicate submissions.
func TestInvalidAndDuplicateSubmissions(t *testing.T) {
	g := newGate()
	svc := newSvc()
	ids := register(t, svc, 3)
	p := startPool(t, svc, 2, 4, g)

	mustSubmit(t, p, ids[0])
	mustSubmit(t, p, ids[1])
	g.waitStarted(t)
	g.waitStarted(t)         // ids[0] and ids[1] are now Processing
	mustSubmit(t, p, ids[2]) // both workers are busy, so ids[2] stays Queued

	if st := mustGet(t, svc, ids[2]).Processing; st != domain.Queued {
		t.Fatalf("Processing = %v, want Queued", st)
	}

	tests := []struct {
		name    string
		id      int
		wantErr error
	}{
		{"already processing", ids[0], domain.ErrInvalidTransition},
		{"already queued", ids[2], domain.ErrInvalidTransition},
		{"unknown player", 9999, domain.ErrPlayerNotFound},
	}
	for _, tt := range tests {
		if err := p.Submit(tt.id); !errors.Is(err, tt.wantErr) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.wantErr)
		}
	}

	g.open()
	p.Shutdown()

	// A completed player cannot be submitted again either. The first pool is
	// stopped, so use a second pool on the same service.
	p2 := startPool(t, svc, 2, 2, nil)
	if err := p2.Submit(ids[0]); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("completed player: err = %v, want ErrInvalidTransition", err)
	}
	for _, id := range ids {
		if st := mustGet(t, svc, id).Processing; st != domain.Completed {
			t.Errorf("player %d: Processing = %v, want Completed (duplicates must not disturb it)", id, st)
		}
	}
}

// A failed evaluation is never reported as Completed, and a panic in the
// evaluator does not kill the worker.
func TestFailedEvaluation(t *testing.T) {
	svc := newSvc()
	good := register(t, svc, 2)
	retired, _ := svc.Register("Old Timer", "Land", domain.DEF, "Home FC")
	if _, err := svc.Retire(retired.ID); err != nil {
		t.Fatal(err)
	}
	boom, _ := svc.Register("Boom", "Land", domain.GK, "Home FC")

	eval := func(p domain.Player) (domain.Report, error) {
		if p.Name == "Boom" {
			panic("kaboom")
		}
		return domain.Evaluate(p)
	}
	pool, err := NewPool(svc, Config{Workers: 2, QueueSize: 10, Evaluate: eval})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Start(); err != nil {
		t.Fatal(err)
	}
	for _, id := range append([]int{retired.ID, boom.ID}, good...) {
		mustSubmit(t, pool, id)
	}
	if got := pool.ActiveWorkers(); got != 2 {
		t.Errorf("ActiveWorkers = %d, want 2 (a panic must not kill a worker)", got)
	}
	pool.Shutdown()

	r := mustGet(t, svc, retired.ID)
	if r.Processing != domain.Failed || !strings.Contains(r.Failure, domain.ErrNotEvaluable.Error()) {
		t.Errorf("retired: Processing=%v Failure=%q, want Failed/not evaluable", r.Processing, r.Failure)
	}
	if r.Report != (domain.Report{}) {
		t.Errorf("failed player must have no report, got %+v", r.Report)
	}
	b := mustGet(t, svc, boom.ID)
	if b.Processing != domain.Failed || !strings.Contains(b.Failure, "kaboom") {
		t.Errorf("panicking: Processing=%v Failure=%q, want Failed/kaboom", b.Processing, b.Failure)
	}
	for _, id := range good {
		if st := mustGet(t, svc, id).Processing; st != domain.Completed {
			t.Errorf("player %d: Processing = %v, want Completed", id, st)
		}
	}
}

// 8. Full queue: rejected, left Pending, and accepted again once there is room.
func TestFullQueueRejectsSubmission(t *testing.T) {
	g := newGate()
	svc := newSvc()
	ids := register(t, svc, 5)
	p := startPool(t, svc, 2, 2, g)

	// Occupy both workers. They have already taken their IDs off the queue.
	mustSubmit(t, p, ids[0])
	mustSubmit(t, p, ids[1])
	g.waitStarted(t)
	g.waitStarted(t)
	// Fill the queue (capacity 2).
	mustSubmit(t, p, ids[2])
	mustSubmit(t, p, ids[3])

	err := p.Submit(ids[4])
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("err = %v, want ErrQueueFull", err)
	}
	rejected := mustGet(t, svc, ids[4])
	if rejected.Processing != domain.Pending {
		t.Errorf("rejected player Processing = %v, want Pending", rejected.Processing)
	}

	// Free the workers; the rejected player can be submitted successfully now.
	g.open()
	deadline := time.Now().Add(hangGuard)
	for {
		err := p.Submit(ids[4])
		if err == nil {
			break
		}
		if !errors.Is(err, ErrQueueFull) {
			t.Fatalf("resubmit: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("queue never drained")
		}
		runtime.Gosched()
	}
	p.Shutdown()

	for _, id := range ids {
		if st := mustGet(t, svc, id).Processing; st != domain.Completed {
			t.Errorf("player %d: Processing = %v, want Completed", id, st)
		}
	}
}

// 9. Shared data is consistent while workers run: readers and other state
// changes happen at the same time as the evaluations. Run with -race.
func TestConcurrentAccessWhileProcessing(t *testing.T) {
	const n = 60
	svc := newSvc()
	ids := register(t, svc, n)
	p := startPool(t, svc, 4, n, nil)

	stop := make(chan struct{})
	var readers sync.WaitGroup
	for r := 0; r < 4; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				svc.FindByNation("Land")
				for _, id := range ids {
					if _, err := svc.Get(id); err != nil {
						t.Errorf("Get(%d): %v", id, err)
						return
					}
				}
			}
		}()
	}

	// Meanwhile every player is loaned out (a different state dimension)
	// while its evaluation is queued/running. Neither update may be lost.
	var writers sync.WaitGroup
	for _, id := range ids {
		id := id
		writers.Add(2)
		go func() {
			defer writers.Done()
			mustSubmit(t, p, id)
		}()
		go func() {
			defer writers.Done()
			if _, err := svc.Loan(id, "Roma"); err != nil {
				t.Errorf("Loan(%d): %v", id, err)
			}
		}()
	}
	writers.Wait()
	p.Shutdown()
	close(stop)
	readers.Wait()

	for _, id := range ids {
		got := mustGet(t, svc, id)
		if got.Processing != domain.Completed {
			t.Errorf("player %d: Processing = %v, want Completed (lost update?)", id, got.Processing)
		}
		if got.Contract != domain.OnLoan || got.LoanClub != "Roma" {
			t.Errorf("player %d: Contract = %v, LoanClub = %q, want OnLoan/Roma (lost update?)", id, got.Contract, got.LoanClub)
		}
		if want, _ := domain.Evaluate(got); got.Report != want {
			t.Errorf("player %d: wrong report %+v", id, got.Report)
		}
	}
}

// 10 and 11. Shutdown: workers exit, new submissions are rejected, repeated
// and concurrent shutdowns are safe, accepted tasks are not lost.
func TestShutdown(t *testing.T) {
	svc := newSvc()
	ids := register(t, svc, 21)
	pool, err := NewPool(svc, Config{Workers: 3, QueueSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Start(); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids[:20] {
		mustSubmit(t, pool, id)
	}

	// Several goroutines shut down at once: no panic from a double close.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.Shutdown()
		}()
	}
	wg.Wait()
	pool.Shutdown() // and once more, sequentially

	if got := pool.ActiveWorkers(); got != 0 {
		t.Errorf("ActiveWorkers after Shutdown = %d, want 0", got)
	}
	for _, id := range ids[:20] {
		if st := mustGet(t, svc, id).Processing; st != domain.Completed {
			t.Errorf("accepted player %d: Processing = %v, want Completed (task lost on shutdown)", id, st)
		}
	}

	// New submissions are rejected and leave the player untouched.
	if err := pool.Submit(ids[20]); !errors.Is(err, ErrPoolStopped) {
		t.Errorf("Submit after Shutdown err = %v, want ErrPoolStopped", err)
	}
	if st := mustGet(t, svc, ids[20]).Processing; st != domain.Pending {
		t.Errorf("rejected player Processing = %v, want Pending", st)
	}
	if err := pool.Start(); !errors.Is(err, ErrPoolStopped) {
		t.Errorf("Start after Shutdown err = %v, want ErrPoolStopped", err)
	}
}

// Submit and Shutdown racing each other must never panic (send on a closed
// channel) and must never lose an accepted task. Repeated to vary interleavings.
func TestConcurrentSubmitAndShutdown(t *testing.T) {
	for round := 0; round < 30; round++ {
		const n = 40
		svc := newSvc()
		ids := register(t, svc, n)
		p, err := NewPool(svc, Config{Workers: 3, QueueSize: 8})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Start(); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		errs := make([]error, n) // each goroutine writes only its own element
		var wg sync.WaitGroup
		for i, id := range ids {
			i, id := i, id // go.mod says Go 1.21: the loop variables are shared between iterations
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = p.Submit(id)
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			p.Shutdown()
		}()
		close(start)
		wg.Wait()
		p.Shutdown() // make sure the drain is finished, whoever got there first

		for i, id := range ids {
			st := mustGet(t, svc, id).Processing
			switch {
			case errs[i] == nil:
				if st != domain.Completed {
					t.Fatalf("round %d: accepted player %d is %v, want Completed", round, id, st)
				}
			case errors.Is(errs[i], ErrQueueFull), errors.Is(errs[i], ErrPoolStopped):
				if st != domain.Pending {
					t.Fatalf("round %d: rejected player %d is %v, want Pending", round, id, st)
				}
			default:
				t.Fatalf("round %d: unexpected Submit error: %v", round, errs[i])
			}
		}
		if got := p.ActiveWorkers(); got != 0 {
			t.Fatalf("round %d: %d workers still alive", round, got)
		}
	}
}
