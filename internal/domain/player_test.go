package domain

import (
	"errors"
	"testing"
)

func TestNewPlayer(t *testing.T) {
	tests := []struct {
		name         string
		inName       string
		inNation     string
		inPos        Position
		inClub       string
		wantErr      error
		wantContract ContractStatus
	}{
		{"valid with club", "Messi", "Argentina", FWD, "Inter Miami", nil, Contracted},
		{"valid without club is free agent", "Kante", "France", MID, "", nil, FreeAgent},
		{"whitespace is trimmed", "  Neuer ", " Germany ", GK, " Bayern ", nil, Contracted},
		{"empty name", "", "Spain", DEF, "Real", ErrInvalidPlayer, 0},
		{"blank name", "   ", "Spain", DEF, "Real", ErrInvalidPlayer, 0},
		{"empty nation", "Pique", "", DEF, "Barca", ErrInvalidPlayer, 0},
		{"invalid position", "Ghost", "Nowhere", Position(42), "FC", ErrInvalidPlayer, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewPlayer(tt.inName, tt.inNation, tt.inPos, tt.inClub)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Contract != tt.wantContract {
				t.Errorf("Contract = %v, want %v", p.Contract, tt.wantContract)
			}
			if p.Availability != Fit {
				t.Errorf("Availability = %v, want Fit", p.Availability)
			}
		})
	}
}

func TestNewPlayerTrimsFields(t *testing.T) {
	p, err := NewPlayer("  Neuer ", " Germany ", GK, " Bayern ")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Neuer" || p.Nation != "Germany" || p.Club != "Bayern" {
		t.Errorf("fields not trimmed: %+v", p)
	}
}

// helper: build a player in a given state, bypassing transitions.
func playerIn(c ContractStatus, a Availability) Player {
	p := Player{Name: "Test", Nation: "Testland", Position: MID, Contract: c, Availability: a}
	switch c {
	case Contracted:
		p.Club = "Home FC"
	case OnLoan:
		p.Club = "Home FC"
		p.LoanClub = "Away FC"
	}
	return p
}

func TestTransitions(t *testing.T) {
	tests := []struct {
		name      string
		start     Player
		action    func(*Player) error
		wantErr   bool
		wantC     ContractStatus
		wantA     Availability
		wantClub  string
		wantLoanC string
	}{
		// --- Availability: valid ---
		{"injure fit", playerIn(Contracted, Fit), (*Player).Injure, false, Contracted, Injured, "Home FC", ""},
		{"injure on loan", playerIn(OnLoan, Fit), (*Player).Injure, false, OnLoan, Injured, "Home FC", "Away FC"},
		{"injure free agent", playerIn(FreeAgent, Fit), (*Player).Injure, false, FreeAgent, Injured, "", ""},
		{"recover injured", playerIn(Contracted, Injured), (*Player).Recover, false, Contracted, Fit, "Home FC", ""},
		{"suspend fit", playerIn(Contracted, Fit), (*Player).Suspend, false, Contracted, Suspended, "Home FC", ""},
		{"end suspension", playerIn(Contracted, Suspended), (*Player).EndSuspension, false, Contracted, Fit, "Home FC", ""},

		// --- Availability: invalid ---
		{"injure already injured", playerIn(Contracted, Injured), (*Player).Injure, true, Contracted, Injured, "Home FC", ""},
		{"injure suspended", playerIn(Contracted, Suspended), (*Player).Injure, true, Contracted, Suspended, "Home FC", ""},
		{"injure retired", playerIn(Retired, Fit), (*Player).Injure, true, Retired, Fit, "", ""},
		{"recover fit", playerIn(Contracted, Fit), (*Player).Recover, true, Contracted, Fit, "Home FC", ""},
		{"suspend injured", playerIn(Contracted, Injured), (*Player).Suspend, true, Contracted, Injured, "Home FC", ""},
		{"suspend free agent", playerIn(FreeAgent, Fit), (*Player).Suspend, true, FreeAgent, Fit, "", ""},
		{"suspend retired", playerIn(Retired, Fit), (*Player).Suspend, true, Retired, Fit, "", ""},
		{"end suspension of fit", playerIn(Contracted, Fit), (*Player).EndSuspension, true, Contracted, Fit, "Home FC", ""},

		// --- Contract: valid ---
		{"loan contracted", playerIn(Contracted, Fit), func(p *Player) error { return p.Loan("Away FC") }, false, OnLoan, Fit, "Home FC", "Away FC"},
		{"loan injured player keeps injury", playerIn(Contracted, Injured), func(p *Player) error { return p.Loan("Away FC") }, false, OnLoan, Injured, "Home FC", "Away FC"},
		{"return from loan", playerIn(OnLoan, Fit), (*Player).ReturnFromLoan, false, Contracted, Fit, "Home FC", ""},
		{"release contracted", playerIn(Contracted, Fit), (*Player).Release, false, FreeAgent, Fit, "", ""},
		{"release suspended clears suspension", playerIn(Contracted, Suspended), (*Player).Release, false, FreeAgent, Fit, "", ""},
		{"release injured keeps injury", playerIn(Contracted, Injured), (*Player).Release, false, FreeAgent, Injured, "", ""},
		{"retire contracted", playerIn(Contracted, Fit), (*Player).Retire, false, Retired, Fit, "", ""},
		{"retire free agent", playerIn(FreeAgent, Fit), (*Player).Retire, false, Retired, Fit, "", ""},

		// --- Contract: invalid ---
		{"loan empty club", playerIn(Contracted, Fit), func(p *Player) error { return p.Loan("  ") }, true, Contracted, Fit, "Home FC", ""},
		{"loan to own club", playerIn(Contracted, Fit), func(p *Player) error { return p.Loan("home fc") }, true, Contracted, Fit, "Home FC", ""},
		{"loan already on loan", playerIn(OnLoan, Fit), func(p *Player) error { return p.Loan("Other FC") }, true, OnLoan, Fit, "Home FC", "Away FC"},
		{"loan free agent", playerIn(FreeAgent, Fit), func(p *Player) error { return p.Loan("Away FC") }, true, FreeAgent, Fit, "", ""},
		{"loan retired", playerIn(Retired, Fit), func(p *Player) error { return p.Loan("Away FC") }, true, Retired, Fit, "", ""},
		{"return when not on loan", playerIn(Contracted, Fit), (*Player).ReturnFromLoan, true, Contracted, Fit, "Home FC", ""},
		{"release on loan", playerIn(OnLoan, Fit), (*Player).Release, true, OnLoan, Fit, "Home FC", "Away FC"},
		{"release free agent", playerIn(FreeAgent, Fit), (*Player).Release, true, FreeAgent, Fit, "", ""},
		{"retire on loan", playerIn(OnLoan, Fit), (*Player).Retire, true, OnLoan, Fit, "Home FC", "Away FC"},
		{"retire already retired", playerIn(Retired, Fit), (*Player).Retire, true, Retired, Fit, "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.start
			err := tt.action(&p)

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("err = %v, want ErrInvalidTransition", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// State must match expectation, and stay unchanged on error.
			if p.Contract != tt.wantC || p.Availability != tt.wantA {
				t.Errorf("state = (%v, %v), want (%v, %v)", p.Contract, p.Availability, tt.wantC, tt.wantA)
			}
			if p.Club != tt.wantClub || p.LoanClub != tt.wantLoanC {
				t.Errorf("clubs = (%q, %q), want (%q, %q)", p.Club, p.LoanClub, tt.wantClub, tt.wantLoanC)
			}
		})
	}
}

// Both dimensions are independent: a loaned player can be injured at the same time.
func TestIndependentDimensions(t *testing.T) {
	p, _ := NewPlayer("Salah", "Egypt", FWD, "Liverpool")
	if err := p.Loan("Roma"); err != nil {
		t.Fatal(err)
	}
	if err := p.Injure(); err != nil {
		t.Fatal(err)
	}
	if p.Contract != OnLoan || p.Availability != Injured {
		t.Fatalf("got (%v, %v), want (OnLoan, Injured)", p.Contract, p.Availability)
	}
}

func TestCanPlay(t *testing.T) {
	tests := []struct {
		name string
		p    Player
		want bool
	}{
		{"contracted and fit", playerIn(Contracted, Fit), true},
		{"on loan and fit", playerIn(OnLoan, Fit), true},
		{"injured", playerIn(Contracted, Injured), false},
		{"suspended", playerIn(Contracted, Suspended), false},
		{"free agent", playerIn(FreeAgent, Fit), false},
		{"retired", playerIn(Retired, Fit), false},
		{"on loan but injured", playerIn(OnLoan, Injured), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.CanPlay(); got != tt.want {
				t.Errorf("CanPlay() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStringMethods(t *testing.T) {
	if Contracted.String() != "Contracted" || OnLoan.String() != "OnLoan" ||
		FreeAgent.String() != "FreeAgent" || Retired.String() != "Retired" {
		t.Error("ContractStatus.String mismatch")
	}
	if ContractStatus(99).String() != "UnknownContractStatus" {
		t.Error("unknown ContractStatus should be reported")
	}
	if Fit.String() != "Fit" || Injured.String() != "Injured" || Suspended.String() != "Suspended" {
		t.Error("Availability.String mismatch")
	}
	if Availability(99).String() != "UnknownAvailability" {
		t.Error("unknown Availability should be reported")
	}
	if GK.String() != "GK" || DEF.String() != "DEF" || MID.String() != "MID" || FWD.String() != "FWD" {
		t.Error("Position.String mismatch")
	}
	if Position(99).String() != "UnknownPosition" {
		t.Error("unknown Position should be reported")
	}
}

func TestPositionValid(t *testing.T) {
	for _, p := range []Position{GK, DEF, MID, FWD} {
		if !p.Valid() {
			t.Errorf("%v should be valid", p)
		}
	}
	if Position(-1).Valid() || Position(4).Valid() {
		t.Error("out-of-range positions must be invalid")
	}
}
