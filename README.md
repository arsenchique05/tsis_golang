# Footballers Registry

Checkpoint 1 – Core Golang. A small in-memory registry of football players where each
player has a lifecycle (state machine): contract changes, injuries, suspensions, loans, retirement.

Standard library only. No concurrency, no HTTP (not required at this stage).

## Run

```bash
go run ./cmd/app     # demo
go test ./...        # unit tests
go test -cover ./... # with coverage
```

Requires Go 1.21+.

## Architecture

Three layers. Dependencies point only downward; `domain` imports nothing.

```
cmd/app/main.go ──► service ──► repository (interface) ◄── memory implementation
                       │                 │
                       └──────► domain ◄─┘
```

```
footballers/
├── cmd/app/main.go                    # demo / wiring
└── internal/
    ├── domain/
    │   ├── player.go                  # Player, NewPlayer, transitions, CanPlay
    │   ├── status.go                  # ContractStatus, Availability, Position
    │   └── errors.go                  # sentinel errors
    ├── repository/
    │   ├── repository.go              # PlayerRepository interface
    │   └── memory.go                  # MemoryRepository (map + nextID)
    └── service/
        └── player_service.go          # use cases: Get -> domain method -> Update
```

## Domain model

A player has two **independent** state dimensions:

| Dimension | Values |
|---|---|
| `ContractStatus` | `Contracted`, `OnLoan`, `FreeAgent`, `Retired` |
| `Availability` | `Fit`, `Injured`, `Suspended` |

So a player can be, for example, `OnLoan` **and** `Injured` at the same time.

### Transition rules

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

`ErrPlayerNotFound`, `ErrDuplicatePlayer`, `ErrInvalidTransition`, `ErrInvalidPlayer`.
They are wrapped with context using `%w`, so use `errors.Is(err, domain.ErrXxx)`.

## Design decisions

- **Pointer receivers for transitions, value receivers for `CanPlay` and `String`.**
  Transitions mutate the player, so they need a pointer. `CanPlay` and `String` only read,
  so a value receiver is enough and safe. (Mixing is acceptable because the pointer methods
  are the "mutating" group and the value methods are read-only helpers.)
- **The repository returns values, not pointers.** The caller gets a copy and cannot corrupt
  the stored data by mutating it. Changes must go through `Update`.
- **`List*` methods sort by ID.** Map iteration order in Go is random, so without sorting
  the output would be unstable. They return non-nil empty slices when nothing matches.
- **Sentinel errors + `%w`.** Layers add context (`injure player 3: ...`) while callers can
  still detect the cause with `errors.Is`.
- **Two status dimensions instead of one enum.** Contract state and availability are
  independent facts. One enum would need combined states (`OnLoanAndInjured`, ...), which
  explodes as states are added.
- **The service depends on an interface.** It can be tested with a fake repository, and the
  storage can be replaced (file, database) without changing the service or the domain.
- **`NewPlayer` validates.** It is the only way to build a valid player; invalid data is
  rejected at the boundary with `ErrInvalidPlayer`.
- **No mutex.** Concurrency is out of scope; `MemoryRepository` is documented as not thread-safe.
