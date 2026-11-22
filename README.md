# Footballers Registry

An in-memory registry of football players. Each player has a lifecycle (state machine):
contract changes, injuries, suspensions, loans, retirement.

- **Checkpoint 1 – Core Golang:** domain model, validation, transitions, repository, service, unit tests.
- **Checkpoint 2 – Concurrent Processing:** players are *evaluated* (rating + market value)
  asynchronously by a **worker pool** fed by a **bounded queue**. Everything from Checkpoint 1 is preserved.

Standard library only. No HTTP, no database (not required).

## Run

```bash
go run ./cmd/app          # demo (registry + worker pool)
go test ./...             # unit tests
go test -race ./...       # unit tests with the race detector
go vet ./...
gofmt -l .                # must print nothing
```

Requires Go 1.21+.

## Architecture

Dependencies point only downward; `domain` imports nothing.

```
cmd/app/main.go ──► processing (worker pool) ──► service ──► repository (interface) ◄── memory implementation
                                                    │                 │
                                                    └──────► domain ◄─┘
```

```
footballers/
├── cmd/app/main.go                    # demo / wiring
└── internal/
    ├── domain/
    │   ├── player.go                  # Player, NewPlayer, transitions (contract, availability, processing)
    │   ├── status.go                  # ContractStatus, Availability, ProcessingStatus, Position
    │   ├── evaluation.go              # Report and the pure Evaluate function (the "processing")
    │   └── errors.go                  # sentinel errors
    ├── repository/
    │   ├── repository.go              # PlayerRepository interface
    │   └── memory.go                  # MemoryRepository (map + nextID, guarded by sync.RWMutex)
    ├── service/
    │   ├── player_service.go          # use cases; apply() = atomic Get -> domain method -> Update
    │   └── processing.go              # MarkQueued / StartProcessing / CompleteProcessing / ...
    └── processing/
        └── pool.go                    # Pool: bounded queue + worker goroutines + Shutdown
```

## Domain model

A player has three **independent** state dimensions:

| Dimension | Values |
|---|---|
| `ContractStatus` | `Contracted`, `OnLoan`, `FreeAgent`, `Retired` |
| `Availability` | `Fit`, `Injured`, `Suspended` |
| `ProcessingStatus` (CP2) | `Pending`, `Queued`, `Processing`, `Completed`, `Failed` |

So a player can be, for example, `OnLoan` **and** `Injured` **and** `Processing` at the same time.

### Transition rules (contract and availability, unchanged from Checkpoint 1)

| Method | Allowed when | Result |
|---|---|---|
| `Injure()` | not retired, `Fit` | `Injured` |
| `Recover()` | `Injured` | `Fit` |
| `Suspend()` | `Contracted`/`OnLoan`, `Fit` | `Suspended` |
| `EndSuspension()` | `Suspended` | `Fit` |
| `Loan(club)` | `Contracted`, club non-empty and not the owning club | `OnLoan` |
| `ReturnFromLoan()` | `OnLoan` | `Contracted` |
| `Release()` | `Contracted` | `FreeAgent` (club cleared, suspension cleared) |
| `Retire()` | `Contracted` or `FreeAgent` | `Retired` (final) |

Any violation returns an error wrapping `ErrInvalidTransition`. A failed transition never
changes the player. `CanPlay()` is true only when the player is `Fit` and `Contracted`/`OnLoan`.

### Errors

`ErrPlayerNotFound`, `ErrDuplicatePlayer`, `ErrInvalidTransition`, `ErrInvalidPlayer`,
`ErrNotEvaluable` (domain) and `ErrQueueFull`, `ErrPoolNotStarted`, `ErrPoolStopped`,
`ErrAlreadyStarted`, `ErrInvalidConfig` (processing).
They are wrapped with context using `%w`, so use `errors.Is(err, ErrXxx)`.

## Checkpoint 2: concurrent processing

### What is processed

The "task" is **evaluating a player**: `domain.Evaluate` computes a `Report` (rating 60–95 and
an estimated market value in million EUR). The rating is derived deterministically from the
player's name and nation. The value depends on the position (forwards are worth more) and is
reduced for injured or suspended players and for free agents. **A retired player cannot be
evaluated**: that is the failure path (`Failed`).

`Evaluate` is a pure function: it reads only a copy of the player and touches no shared state,
so workers run it without holding any lock.

### How tasks are created and submitted

1. `service.Register(...)` creates and validates a player (`Pending`), exactly as in Checkpoint 1.
2. `pool.Submit(playerID)` puts the player in the queue and **returns immediately**; it does not
   wait for the evaluation.
3. A worker evaluates it later; read the outcome with `service.Get(id)` at any time.

### Worker pool

```
                      ┌────────────────────────────┐
 Submit(id) ────────► │ bounded queue (chan int)   │
 Submit(id) ────────► │ capacity = QueueSize       │
                      └───────────┬────────────────┘
                     ┌────────────┼────────────┐
                     ▼            ▼            ▼
                  worker 1     worker 2     worker N      (fixed number, reused)
                     └────────────┼────────────┘
                                  ▼
              PlayerService (short critical sections, mutex)
                                  ▼
                    MemoryRepository (RWMutex) = task storage
```

- `Config{Workers, QueueSize, Evaluate}`. **Workers must be ≥ 2**; the demo uses **3**.
- **Queue capacity** is `QueueSize` (a buffered channel); the demo uses **8**.
- Workers are started once (`Start`) and keep taking IDs from the queue until it is closed and empty.
  There is never one goroutine per task, so concurrency is bounded by the number of workers.
- The queue carries player **IDs**, not players: the repository stays the single source of truth,
  and a worker always re-reads the current player.
- Order of completion is not defined and nothing depends on it.

### What happens when the queue is full

`Submit` **never blocks**. If the queue has no free slot it returns `ErrQueueFull`, and the player
is rolled back to `Pending` (it is not left `Queued` or `Processing`). The caller may retry later.
Because `Submit` never blocks, a caller can never be stuck forever on a pool that has stopped.

### Processing lifecycle (`ProcessingStatus`)

```
Pending ──Submit──► Queued ──worker claims──► Processing ──► Completed (Report stored)
   ▲                  │                            └──────► Failed    (Failure reason stored)
   └──queue full──────┘
```

| Transition | Method | Who |
|---|---|---|
| `Pending → Queued` | `Enqueue` | `Submit` |
| `Queued → Pending` | `Dequeue` | `Submit`, only when the queue is full |
| `Queued → Processing` | `StartProcessing` | a worker (the *claim*) |
| `Processing → Completed` | `CompleteProcessing(report)` | the worker |
| `Processing → Failed` | `FailProcessing(reason)` | the worker (also if the evaluator panics) |

Every other combination returns `ErrInvalidTransition`. In particular, submitting a player that is
already `Queued`, `Processing`, `Completed` or `Failed` is an error: **there is no reprocessing**.
A failed evaluation is never marked `Completed`.

### How shared data is synchronized

| Shared data | Protected by | Why |
|---|---|---|
| the `players` map and `nextID` | `MemoryRepository.mu` (`sync.RWMutex`) | A Go map must never be read and written concurrently, not even for different keys. Reads share `RLock`; writes take `Lock`. `Add` does the duplicate check and the insert under one `Lock`. |
| read → change → write of one player | `PlayerService.mu` (`sync.Mutex`) in `apply` | The repository lock covers only single calls. Without this, two goroutines could `Get` the same player, change different fields, and the later `Update` would overwrite the earlier one (a lost update, which `-race` cannot see). Every state change is therefore atomic. |
| claim "only one worker per player" | `StartProcessing` is `Queued → Processing` inside `apply` | Only one caller can win; the others get `ErrInvalidTransition`. |
| pool state and the queue channel | `Pool.mu` (`sync.RWMutex`) | `Submit` holds `RLock` while it checks the state and sends; `Shutdown` takes `Lock` to flip the state and close the channel. So a send on a closed channel is impossible. |
| live-worker counter | `sync/atomic` | Readable at any time without a lock. |

- Repository methods return **copies** (`domain.Player` by value), so callers cannot modify stored data.
- **No lock is held during an evaluation**: critical sections are a few field assignments.
- Lock order is always `Pool.mu → PlayerService.mu → MemoryRepository.mu`, and no lower layer
  calls a higher one, so there are no cycles and no deadlock.

### How workers are stopped

`pool.Shutdown()`:

1. takes `Pool.mu.Lock()`, sets the state to *stopped* and closes the queue (exactly once; later calls skip this);
2. from now on `Submit` returns `ErrPoolStopped` and does not touch the player;
3. workers still receive everything already in the buffered queue (`close` does not discard items),
   then their `for id := range queue` loop ends;
4. `Shutdown` waits on a `sync.WaitGroup` until every worker has exited.

So accepted tasks are never lost, workers do not keep running, and `Shutdown` can be called more than
once, or concurrently with `Submit`, without panicking. The `Pool` owns the channel and is the only
one that closes it; workers only receive.

### Tests

```bash
go test -race -count=1 ./...
```

- `internal/processing/pool_test.go`: startup and config validation, asynchronous submission, results of
  many tasks, **proof of overlapping evaluations** (a gate evaluator blocks two workers at the same time,
  no `time.Sleep`), worker limit respected, duplicate/invalid submissions, failed evaluations and a
  panicking evaluator, full queue (rejected, `Pending`, accepted later), concurrent reads and writes while
  processing, shutdown (workers terminate, repeated and concurrent calls, rejection afterwards), and
  concurrent `Submit` vs `Shutdown`.
- `internal/repository/memory_concurrency_test.go`: unique IDs and a single winner for duplicates under concurrency.
- `internal/service/player_service_concurrency_test.go`: the lost-update test and the "only one `MarkQueued` wins" test.
- All Checkpoint 1 tests still pass unchanged.

## Design decisions

- **Pointer receivers for transitions, value receivers for `CanPlay` and `String`.**
  Transitions mutate the player, so they need a pointer. `CanPlay` and `String` only read.
- **The repository returns values, not pointers.** The caller gets a copy and cannot corrupt
  the stored data. Changes go through `Update` (or the service).
- **`List*` methods sort by ID.** Map iteration order in Go is random, so without sorting
  the output would be unstable. They return non-nil empty slices when nothing matches.
- **Sentinel errors + `%w`.** Layers add context while callers detect the cause with `errors.Is`.
- **Independent status dimensions instead of one enum.** A single enum would need combined states
  (`OnLoanAndInjuredAndProcessing`, ...). The processing status is simply a third dimension.
- **The service depends on an interface.** It can be tested with a fake repository, and the
  storage can be replaced without changing the service or the domain.
- **`NewPlayer` validates.** It is the only way to build a valid player.
- **Reject instead of block on a full queue.** Simple, predictable, and a caller can never hang.
- **Mutexes instead of a single owner goroutine.** The shared state is a small map with tiny
  critical sections, so a mutex is the simplest correct tool; channels are used where they fit
  naturally: handing work from submitters to workers.
- **Known limit:** an evaluation works on a snapshot taken when the worker claims the player. If the
  player changes while it is being evaluated (for example he gets injured), the report reflects the
  state at claim time.
