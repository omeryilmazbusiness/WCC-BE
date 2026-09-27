package payment

import (
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
)

// Clock yields calendar dates in the business time zone (received_at,
// promised_on). The zero value uses UTC and the wall clock.
type Clock struct {
	Location *time.Location
	Now      func() time.Time
}

func (c Clock) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Today is the current calendar date at UTC midnight.
func (c Clock) Today() time.Time {
	loc := c.Location
	if loc == nil {
		loc = time.UTC
	}
	return fx.DateOf(c.now().In(loc))
}
