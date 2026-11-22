package domain

import (
	"errors"
	"testing"
)

func TestProcessingHappyPath(t *testing.T) {
	p := playerIn(Contracted, Fit)
	if p.Processing != Pending {
		t.Fatalf("new player Processing = %v, want Pending", p.Processing)
	}

	rep, err := Evaluate(p)
	if err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		name string
		do   func() error
		want ProcessingStatus
	}{
		{"enqueue", p.Enqueue, Queued},
		{"start", p.StartProcessing, Processing},
		{"complete", func() error { return p.CompleteProcessing(rep) }, Completed},
	}
	for _, s := range steps {
		if err := s.do(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if p.Processing != s.want {
			t.Fatalf("%s: Processing = %v, want %v", s.name, p.Processing, s.want)
		}
	}
	if p.Report != rep {
		t.Errorf("Report = %+v, want %+v", p.Report, rep)
	}
}

func TestProcessingFailurePath(t *testing.T) {
	p := playerIn(Contracted, Fit)
	if err := p.Enqueue(); err != nil {
		t.Fatal(err)
	}
	if err := p.StartProcessing(); err != nil {
		t.Fatal(err)
	}
	if err := p.FailProcessing("boom"); err != nil {
		t.Fatal(err)
	}
	if p.Processing != Failed || p.Failure != "boom" || p.Report != (Report{}) {
		t.Errorf("unexpected state after failure: %+v", p)
	}
}

func TestDequeueRollsBackEnqueue(t *testing.T) {
	p := playerIn(Contracted, Fit)
	if err := p.Enqueue(); err != nil {
		t.Fatal(err)
	}
	if err := p.Dequeue(); err != nil {
		t.Fatal(err)
	}
	if p.Processing != Pending {
		t.Errorf("Processing = %v, want Pending", p.Processing)
	}
}

// Every transition is only legal from exactly one status. This table checks
// all the illegal combinations and that a failed transition changes nothing.
func TestProcessingInvalidTransitions(t *testing.T) {
	all := []ProcessingStatus{Pending, Queued, Processing, Completed, Failed}
	ops := []struct {
		name string
		from ProcessingStatus // the only legal source status
		do   func(*Player) error
	}{
		{"Enqueue", Pending, (*Player).Enqueue},
		{"Dequeue", Queued, (*Player).Dequeue},
		{"StartProcessing", Queued, (*Player).StartProcessing},
		{"CompleteProcessing", Processing, func(p *Player) error { return p.CompleteProcessing(Report{Rating: 70}) }},
		{"FailProcessing", Processing, func(p *Player) error { return p.FailProcessing("x") }},
	}
	for _, op := range ops {
		for _, st := range all {
			if st == op.from {
				continue
			}
			p := playerIn(Contracted, Fit)
			p.Processing = st
			before := p
			err := op.do(&p)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("%s from %v: err = %v, want ErrInvalidTransition", op.name, st, err)
			}
			if p != before {
				t.Errorf("%s from %v: player changed on failure: %+v", op.name, st, p)
			}
		}
	}
}

func TestEvaluate(t *testing.T) {
	t.Run("deterministic and in range", func(t *testing.T) {
		p := playerIn(Contracted, Fit)
		a, err := Evaluate(p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := Evaluate(p)
		if a != b {
			t.Errorf("not deterministic: %+v vs %+v", a, b)
		}
		if a.Rating < 60 || a.Rating > 95 {
			t.Errorf("Rating = %d, want 60..95", a.Rating)
		}
		if a.MarketValue <= 0 || a.Summary == "" {
			t.Errorf("bad report: %+v", a)
		}
	})

	t.Run("different players get different reports", func(t *testing.T) {
		seen := map[Report]bool{}
		for _, name := range []string{"Messi", "Mbappe", "Kante", "Salah", "Haaland"} {
			p, _ := NewPlayer(name, "Land", MID, "FC")
			r, err := Evaluate(p)
			if err != nil {
				t.Fatal(err)
			}
			seen[r] = true
		}
		if len(seen) < 3 {
			t.Errorf("reports are suspiciously identical: %d distinct", len(seen))
		}
	})

	t.Run("injury and free agency lower the value", func(t *testing.T) {
		fit, _ := Evaluate(playerIn(Contracted, Fit))
		injured, _ := Evaluate(playerIn(Contracted, Injured))
		free, _ := Evaluate(playerIn(FreeAgent, Fit))
		if injured.MarketValue >= fit.MarketValue {
			t.Errorf("injured %d should be < fit %d", injured.MarketValue, fit.MarketValue)
		}
		if free.MarketValue >= fit.MarketValue {
			t.Errorf("free agent %d should be < contracted %d", free.MarketValue, fit.MarketValue)
		}
		if injured.Rating != fit.Rating {
			t.Error("availability must not change the rating")
		}
	})

	t.Run("retired player is not evaluable", func(t *testing.T) {
		_, err := Evaluate(playerIn(Retired, Fit))
		if !errors.Is(err, ErrNotEvaluable) {
			t.Errorf("err = %v, want ErrNotEvaluable", err)
		}
	})
}

func TestProcessingStatusString(t *testing.T) {
	want := map[ProcessingStatus]string{
		Pending: "Pending", Queued: "Queued", Processing: "Processing",
		Completed: "Completed", Failed: "Failed", ProcessingStatus(99): "UnknownProcessingStatus",
	}
	for st, s := range want {
		if st.String() != s {
			t.Errorf("%d.String() = %q, want %q", int(st), st.String(), s)
		}
	}
}
