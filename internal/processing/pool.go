// Package processing contains the worker pool that evaluates players
// asynchronously (Checkpoint 2).
//
// Architecture:
//
//	Submit(id) ──► bounded channel (queue) ──► worker 1 ┐
//	                                           worker 2 ├──► PlayerService (locks) ──► repository
//	                                           worker N ┘
//
// Ownership: the Pool OWNS the queue channel. Only Pool.Shutdown closes it, and
// only after it has stopped accepting submissions. Workers only receive.
package processing

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"footballers/internal/domain"
	"footballers/internal/service"
)

var (
	// ErrQueueFull is returned by Submit when the queue has no free slot.
	// Submit never blocks: a full queue means "reject".
	ErrQueueFull = errors.New("task queue is full")
	// ErrPoolNotStarted is returned by Submit before Start was called.
	ErrPoolNotStarted = errors.New("worker pool is not started")
	// ErrPoolStopped is returned by Submit and Start after Shutdown.
	ErrPoolStopped = errors.New("worker pool is stopped")
	// ErrAlreadyStarted is returned when Start is called twice.
	ErrAlreadyStarted = errors.New("worker pool is already started")
	// ErrInvalidConfig is returned by NewPool for unusable settings.
	ErrInvalidConfig = errors.New("invalid pool configuration")
)

// MinWorkers is the smallest allowed pool size (the specification requires at least 2).
const MinWorkers = 2

// Evaluator turns a player snapshot into a report. It must not touch shared state.
type Evaluator func(domain.Player) (domain.Report, error)

// Config describes a pool.
type Config struct {
	Workers   int       // number of worker goroutines, >= MinWorkers
	QueueSize int       // capacity of the bounded queue, >= 1
	Evaluate  Evaluator // nil means domain.Evaluate
}

type state int

const (
	stateNew state = iota
	stateRunning
	stateStopped
)

// Pool is a fixed set of worker goroutines fed by a bounded queue of player IDs.
type Pool struct {
	svc     *service.PlayerService
	eval    Evaluator
	workers int

	// queue carries player IDs (not players): the single source of truth for a
	// player's data stays in the repository, workers always re-read it.
	queue chan int

	// mu guards state AND the act of sending to / closing queue. Submit holds
	// the read lock while it checks the state and sends; Shutdown takes the
	// write lock to flip the state and close the channel. So a send can never
	// happen on a closed channel.
	mu    sync.RWMutex
	state state

	wg   sync.WaitGroup // counts live worker goroutines
	live atomic.Int32   // same number, readable at any time (see ActiveWorkers)
}

// NewPool validates the configuration and builds a pool. Workers are not
// running until Start is called.
func NewPool(svc *service.PlayerService, cfg Config) (*Pool, error) {
	if svc == nil {
		return nil, fmt.Errorf("%w: service is nil", ErrInvalidConfig)
	}
	if cfg.Workers < MinWorkers {
		return nil, fmt.Errorf("%w: need at least %d workers, got %d", ErrInvalidConfig, MinWorkers, cfg.Workers)
	}
	if cfg.QueueSize < 1 {
		return nil, fmt.Errorf("%w: queue size must be >= 1, got %d", ErrInvalidConfig, cfg.QueueSize)
	}
	eval := cfg.Evaluate
	if eval == nil {
		eval = domain.Evaluate
	}
	return &Pool{
		svc:     svc,
		eval:    eval,
		workers: cfg.Workers,
		queue:   make(chan int, cfg.QueueSize),
	}, nil
}

// Start launches the worker goroutines. It can be called only once.
func (p *Pool) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch p.state {
	case stateRunning:
		return ErrAlreadyStarted
	case stateStopped:
		return ErrPoolStopped
	}
	p.state = stateRunning

	// Add BEFORE starting the goroutines: if Add happened inside them, Shutdown's
	// Wait could run first and return while workers were still about to start.
	p.wg.Add(p.workers)
	p.live.Add(int32(p.workers))
	for i := 0; i < p.workers; i++ {
		go p.worker()
	}
	return nil
}

// Submit puts a player in the queue and returns immediately; it does not wait
// for the evaluation. Behaviour when the queue is full: it does NOT block, it
// returns ErrQueueFull and leaves the player Pending.
//
// Errors: ErrPoolNotStarted, ErrPoolStopped, ErrQueueFull, plus
// domain.ErrPlayerNotFound / domain.ErrInvalidTransition from the service
// (unknown player, or player already queued/processing/done).
func (p *Pool) Submit(id int) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	switch p.state {
	case stateNew:
		return ErrPoolNotStarted
	case stateStopped:
		return ErrPoolStopped
	}

	// Pending -> Queued BEFORE the send: once the ID is in the channel a worker
	// may take it immediately, and it expects to find the player Queued.
	if _, err := p.svc.MarkQueued(id); err != nil {
		return fmt.Errorf("submit player %d: %w", id, err)
	}

	select {
	case p.queue <- id:
		return nil
	default:
		// Queue full. No worker has seen this ID, so rolling back is safe.
		if _, err := p.svc.UnmarkQueued(id); err != nil {
			return fmt.Errorf("submit player %d: %w", id, errors.Join(ErrQueueFull, err))
		}
		return fmt.Errorf("submit player %d: %w", id, ErrQueueFull)
	}
}

// Shutdown stops accepting new submissions, lets the workers finish every task
// that was already accepted, and waits until all workers have exited.
// It is safe to call concurrently with Submit and safe to call more than once
// (later calls just wait for the same shutdown to finish).
func (p *Pool) Shutdown() {
	p.mu.Lock()
	if p.state != stateStopped {
		p.state = stateStopped
		// Only this branch ever closes the channel, and it runs at most once.
		// close() does not discard buffered items: workers still receive them.
		close(p.queue)
	}
	p.mu.Unlock()

	// Wait outside the lock: workers never take p.mu, but there is no reason to
	// make Submit callers wait for the whole drain.
	p.wg.Wait()
}

// ActiveWorkers returns how many worker goroutines are currently alive.
func (p *Pool) ActiveWorkers() int { return int(p.live.Load()) }

// worker takes IDs until the queue is closed AND empty, then exits.
func (p *Pool) worker() {
	defer p.wg.Done() // registered first, so it runs last
	defer p.live.Add(-1)

	for id := range p.queue {
		p.process(id)
	}
}

// process runs one evaluation. All shared-state changes go through the service
// (short critical sections); the evaluation itself runs with NO lock held.
func (p *Pool) process(id int) {
	// Claim the player. If this fails the player is not Queued any more (for
	// example it was deleted); there is nothing for this worker to do.
	player, err := p.svc.StartProcessing(id)
	if err != nil {
		return
	}

	report, err := p.evaluate(player)
	if err != nil {
		// Never mark a failed evaluation as Completed. If this update fails the
		// player was deleted meanwhile and there is nobody left to inform.
		_, _ = p.svc.FailProcessing(id, err.Error())
		return
	}
	_, _ = p.svc.CompleteProcessing(id, report)
}

// evaluate calls the evaluator and turns a panic into an error, so one bad
// task cannot kill a worker (and, through it, the whole program).
func (p *Pool) evaluate(player domain.Player) (r domain.Report, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("evaluator panic: %v", rec)
		}
	}()
	return p.eval(player)
}
