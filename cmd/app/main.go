// Command app is a small demo of the footballers registry.
package main

import (
	"errors"
	"fmt"
	"log"

	"footballers/internal/domain"
	"footballers/internal/processing"
	"footballers/internal/repository"
	"footballers/internal/service"
)

func main() {
	// Wiring: the concrete storage is chosen here, and only here.
	svc := service.NewPlayerService(repository.NewMemoryRepository())

	// 1. Register players.
	type seed struct {
		name, nation string
		pos          domain.Position
		club         string
	}
	seeds := []seed{
		{"Lionel Messi", "Argentina", domain.FWD, "Inter Miami"},
		{"Emiliano Martinez", "Argentina", domain.GK, "Aston Villa"},
		{"Kylian Mbappe", "France", domain.FWD, "Real Madrid"},
		{"N'Golo Kante", "France", domain.MID, "Al Ittihad"},
	}
	ids := make([]int, 0, len(seeds))
	for _, s := range seeds {
		p, err := svc.Register(s.name, s.nation, s.pos, s.club)
		if err != nil {
			log.Fatalf("register: %v", err)
		}
		ids = append(ids, p.ID)
	}
	fmt.Println("== Registered players ==")
	printAll(svc, ids)

	// 2. State changes.
	fmt.Println("\n== Injure Messi, loan Kante to Chelsea ==")
	must(svc.Injure(ids[0]))
	must(svc.Loan(ids[3], "Chelsea"))
	printAll(svc, ids)

	// 3. Independent dimensions: a loaned player can also be injured.
	fmt.Println("\n== Injure the loaned Kante (OnLoan + Injured at once) ==")
	must(svc.Injure(ids[3]))
	printAll(svc, ids)

	// 4. An invalid transition: errors.Is sees the sentinel through the wrapping.
	fmt.Println("\n== Invalid transition: injure Messi again ==")
	if _, err := svc.Injure(ids[0]); err != nil {
		fmt.Println("error:", err)
		fmt.Println("is ErrInvalidTransition:", errors.Is(err, domain.ErrInvalidTransition))
	}

	// 5. Not found.
	fmt.Println("\n== Unknown player ==")
	if _, err := svc.Get(999); err != nil {
		fmt.Println("error:", err)
		fmt.Println("is ErrPlayerNotFound:", errors.Is(err, domain.ErrPlayerNotFound))
	}

	// 6. Queries.
	fmt.Println("\n== French players ==")
	for _, p := range svc.FindByNation("france") {
		fmt.Println(format(p))
	}
	fmt.Println("\n== Forwards ==")
	for _, p := range svc.FindByPosition(domain.FWD) {
		fmt.Println(format(p))
	}

	// 7. Checkpoint 2: evaluate the players concurrently with a worker pool.
	retired := must(svc.Register("Zlatan Ibrahimovic", "Sweden", domain.FWD, "AC Milan"))
	must(svc.Retire(retired.ID)) // a retired player cannot be evaluated -> Failed
	ids = append(ids, retired.ID)

	fmt.Println("\n== Worker pool: 3 workers, queue capacity 8 ==")
	pool, err := processing.NewPool(svc, processing.Config{Workers: 3, QueueSize: 8})
	if err != nil {
		log.Fatalf("new pool: %v", err)
	}
	if err := pool.Start(); err != nil {
		log.Fatalf("start pool: %v", err)
	}
	fmt.Println("workers running:", pool.ActiveWorkers())

	for _, id := range ids {
		if err := pool.Submit(id); err != nil { // returns immediately, does not wait
			log.Fatalf("submit: %v", err)
		}
	}
	// Submitting the same player again is rejected, whatever stage it is in.
	if err := pool.Submit(ids[0]); err != nil {
		fmt.Println("duplicate submit:", err)
		fmt.Println("is ErrInvalidTransition:", errors.Is(err, domain.ErrInvalidTransition))
	}

	// Shutdown stops accepting tasks, lets the workers finish everything that
	// was accepted, and waits for them to exit. Afterwards all results are final.
	pool.Shutdown()
	fmt.Println("workers running after shutdown:", pool.ActiveWorkers())

	fmt.Println("\n== Evaluation results ==")
	for _, id := range ids {
		p, err := svc.Get(id)
		if err != nil {
			log.Fatalf("get: %v", err)
		}
		switch p.Processing {
		case domain.Completed:
			fmt.Printf("#%d %-18s %-10s %s\n", p.ID, p.Name, p.Processing, p.Report.Summary)
		default:
			fmt.Printf("#%d %-18s %-10s %s\n", p.ID, p.Name, p.Processing, p.Failure)
		}
	}

	fmt.Println("\n== Submit after shutdown ==")
	if err := pool.Submit(ids[0]); err != nil {
		fmt.Println("error:", err)
		fmt.Println("is ErrPoolStopped:", errors.Is(err, processing.ErrPoolStopped))
	}
}

func must(p domain.Player, err error) domain.Player {
	if err != nil {
		log.Fatalf("unexpected error: %v", err)
	}
	return p
}

func printAll(svc *service.PlayerService, ids []int) {
	for _, id := range ids {
		p, err := svc.Get(id)
		if err != nil {
			log.Fatalf("get: %v", err)
		}
		fmt.Printf("%s | CanPlay=%v\n", format(p), p.CanPlay())
	}
}

func format(p domain.Player) string {
	club := p.Club
	if p.Contract == domain.OnLoan {
		club = fmt.Sprintf("%s -> %s", p.Club, p.LoanClub)
	}
	if club == "" {
		club = "-"
	}
	return fmt.Sprintf("#%d %-18s %-10s %-3s %-10s %-9s club: %s",
		p.ID, p.Name, p.Nation, p.Position, p.Contract, p.Availability, club)
}
