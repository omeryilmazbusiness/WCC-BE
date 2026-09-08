package clock

import "time"

// Clock abstracts time for testable domain/application logic.
type Clock interface {
	Now() time.Time
}

type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Fixed returns a deterministic clock for tests.
type Fixed struct{ T time.Time }

func (f Fixed) Now() time.Time { return f.T }
