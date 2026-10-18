// Command app is a small demo of the footballers registry.
package main

import (
	"errors"
	"fmt"
	"log"

	"footballers/internal/domain"
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
