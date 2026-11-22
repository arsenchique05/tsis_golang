package domain

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// Report is the result of evaluating a player.
type Report struct {
	Rating      int    // 60..95, derived deterministically from name and nation
	MarketValue int    // estimated value in million EUR
	Summary     string // human readable one-liner
}

// Evaluate is the domain-specific "processing" of a player: it estimates a
// rating and a market value.
//
// It is a PURE function: it reads only its argument (a copy) and touches no
// shared state. That is why workers can run it concurrently, without holding
// any lock, while the rest of the program keeps using the registry.
//
// Retired players cannot be evaluated: this is the failure path of the lifecycle.
func Evaluate(p Player) (Report, error) {
	if p.Contract == Retired {
		return Report{}, fmt.Errorf("%w: %q is retired", ErrNotEvaluable, p.Name)
	}

	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(p.Name + "|" + p.Nation))) // hash.Hash.Write never returns an error
	rating := 60 + int(h.Sum32()%36)

	value := (rating - 50) * 3 // million EUR

	switch p.Position {
	case FWD:
		value = value * 120 / 100
	case DEF:
		value = value * 90 / 100
	case GK:
		value = value * 80 / 100
	}
	switch p.Availability {
	case Injured:
		value = value * 60 / 100
	case Suspended:
		value = value * 90 / 100
	}
	if p.Contract == FreeAgent {
		value = value * 80 / 100
	}

	return Report{
		Rating:      rating,
		MarketValue: value,
		Summary:     fmt.Sprintf("%s %s: rating %d, value %dM EUR", p.Position, p.Name, rating, value),
	}, nil
}
