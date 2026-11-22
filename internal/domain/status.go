package domain

// ContractStatus describes where a player "belongs" contractually.
// It is independent of Availability: a loaned player can also be injured.
type ContractStatus int

const (
	Contracted ContractStatus = iota
	OnLoan
	FreeAgent
	Retired
)

// String implements fmt.Stringer. Value receiver: it only reads the value.
func (c ContractStatus) String() string {
	switch c {
	case Contracted:
		return "Contracted"
	case OnLoan:
		return "OnLoan"
	case FreeAgent:
		return "FreeAgent"
	case Retired:
		return "Retired"
	default:
		return "UnknownContractStatus"
	}
}

// Availability describes whether a player is physically/disciplinarily able to play.
type Availability int

const (
	Fit Availability = iota
	Injured
	Suspended
)

// String implements fmt.Stringer.
func (a Availability) String() string {
	switch a {
	case Fit:
		return "Fit"
	case Injured:
		return "Injured"
	case Suspended:
		return "Suspended"
	default:
		return "UnknownAvailability"
	}
}

// ProcessingStatus is the third, independent state dimension added in
// Checkpoint 2. It tracks the asynchronous evaluation of a player:
//
//	Pending -> Queued -> Processing -> Completed
//	                          \-> Failed
//
// Pending is the zero value, so a freshly created player is "not submitted yet".
type ProcessingStatus int

const (
	Pending ProcessingStatus = iota
	Queued
	Processing
	Completed
	Failed
)

// String implements fmt.Stringer.
func (s ProcessingStatus) String() string {
	switch s {
	case Pending:
		return "Pending"
	case Queued:
		return "Queued"
	case Processing:
		return "Processing"
	case Completed:
		return "Completed"
	case Failed:
		return "Failed"
	default:
		return "UnknownProcessingStatus"
	}
}

// Position is the player's role on the pitch.
type Position int

const (
	GK Position = iota
	DEF
	MID
	FWD
)

// String implements fmt.Stringer.
func (p Position) String() string {
	switch p {
	case GK:
		return "GK"
	case DEF:
		return "DEF"
	case MID:
		return "MID"
	case FWD:
		return "FWD"
	default:
		return "UnknownPosition"
	}
}

// Valid reports whether p is one of the defined positions.
func (p Position) Valid() bool {
	return p >= GK && p <= FWD
}
