package domain

import (
	"fmt"
	"strings"
)

// Player is the core entity. Its lifecycle is modelled with two independent
// state dimensions (Contract and Availability), so combinations such as
// "OnLoan + Injured" are representable.
type Player struct {
	ID           int
	Name         string
	Nation       string
	Position     Position
	Contract     ContractStatus
	Availability Availability
	Club         string // owning club; empty for FreeAgent
	LoanClub     string // club the player is loaned to; only set while OnLoan
}

// NewPlayer validates the input and builds a Player.
// It returns a value (not a pointer): Player is small and the caller decides
// whether it needs a pointer. ID is assigned later by the repository.
// If club is empty the player starts as a FreeAgent, otherwise as Contracted.
func NewPlayer(name, nation string, pos Position, club string) (Player, error) {
	name = strings.TrimSpace(name)
	nation = strings.TrimSpace(nation)
	club = strings.TrimSpace(club)

	if name == "" {
		return Player{}, fmt.Errorf("%w: name must not be empty", ErrInvalidPlayer)
	}
	if nation == "" {
		return Player{}, fmt.Errorf("%w: nation must not be empty", ErrInvalidPlayer)
	}
	if !pos.Valid() {
		return Player{}, fmt.Errorf("%w: unknown position %d", ErrInvalidPlayer, int(pos))
	}

	p := Player{
		Name:         name,
		Nation:       nation,
		Position:     pos,
		Contract:     Contracted,
		Availability: Fit,
		Club:         club,
	}
	if club == "" {
		p.Contract = FreeAgent
	}
	return p, nil
}

// CanPlay reports whether the player can be selected for a match.
// Value receiver: it does not modify the player.
func (p Player) CanPlay() bool {
	return p.Availability == Fit &&
		(p.Contract == Contracted || p.Contract == OnLoan)
}

// --- Availability transitions (pointer receivers: they mutate the player) ---

// Injure marks a fit player as injured.
func (p *Player) Injure() error {
	if p.Contract == Retired {
		return p.invalid("injure", "player is retired")
	}
	if p.Availability != Fit {
		return p.invalid("injure", "player is "+p.Availability.String())
	}
	p.Availability = Injured
	return nil
}

// Recover returns an injured player to fit.
func (p *Player) Recover() error {
	if p.Availability != Injured {
		return p.invalid("recover", "player is not injured")
	}
	p.Availability = Fit
	return nil
}

// Suspend marks a fit player as suspended (disciplinary).
func (p *Player) Suspend() error {
	switch {
	case p.Contract == Retired:
		return p.invalid("suspend", "player is retired")
	case p.Contract == FreeAgent:
		return p.invalid("suspend", "free agent has no club to be suspended from")
	case p.Availability != Fit:
		return p.invalid("suspend", "player is "+p.Availability.String())
	}
	p.Availability = Suspended
	return nil
}

// EndSuspension returns a suspended player to fit.
func (p *Player) EndSuspension() error {
	if p.Availability != Suspended {
		return p.invalid("end suspension", "player is not suspended")
	}
	p.Availability = Fit
	return nil
}

// --- Contract transitions ---

// Loan sends a contracted player to another club.
func (p *Player) Loan(club string) error {
	club = strings.TrimSpace(club)
	switch {
	case p.Contract != Contracted:
		return p.invalid("loan", "player is "+p.Contract.String())
	case club == "":
		return p.invalid("loan", "loan club must not be empty")
	case strings.EqualFold(club, p.Club):
		return p.invalid("loan", "cannot loan to the owning club")
	}
	p.Contract = OnLoan
	p.LoanClub = club
	return nil
}

// ReturnFromLoan brings a loaned player back to the owning club.
func (p *Player) ReturnFromLoan() error {
	if p.Contract != OnLoan {
		return p.invalid("return from loan", "player is not on loan")
	}
	p.Contract = Contracted
	p.LoanClub = ""
	return nil
}

// Release terminates the contract and makes the player a free agent.
// A suspension is tied to the club, so it is cleared too; an injury stays.
func (p *Player) Release() error {
	if p.Contract != Contracted {
		return p.invalid("release", "player is "+p.Contract.String())
	}
	p.Contract = FreeAgent
	p.Club = ""
	if p.Availability == Suspended {
		p.Availability = Fit
	}
	return nil
}

// Retire moves a player to the final Retired state.
func (p *Player) Retire() error {
	if p.Contract != Contracted && p.Contract != FreeAgent {
		return p.invalid("retire", "player is "+p.Contract.String())
	}
	p.Contract = Retired
	p.Club = ""
	p.LoanClub = ""
	p.Availability = Fit
	return nil
}

// invalid builds an ErrInvalidTransition wrapped with context.
// %w keeps the sentinel visible to errors.Is.
func (p *Player) invalid(action, reason string) error {
	return fmt.Errorf("%w: cannot %s %q: %s", ErrInvalidTransition, action, p.Name, reason)
}
