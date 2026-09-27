// Package authsec holds pure authentication-security rules: lockout policy,
// refresh-token rotation decisions, recovery codes and opaque tokens.
package authsec

import "time"

// LockoutPolicy locks an account after MaxAttempts failures inside Window.
// Each consecutive lockout doubles Base, capped at Max.
type LockoutPolicy struct {
	MaxAttempts int
	Window      time.Duration
	Base        time.Duration
	Max         time.Duration
}

// ShouldLock reports whether failures within the window reach the threshold.
func (p LockoutPolicy) ShouldLock(failures int) bool {
	return p.MaxAttempts > 0 && failures >= p.MaxAttempts
}

// LockDuration is Base * 2^priorLockouts, capped at Max.
func (p LockoutPolicy) LockDuration(priorLockouts int) time.Duration {
	d := p.Base
	if d <= 0 {
		return 0
	}
	for i := 0; i < priorLockouts; i++ {
		if p.Max > 0 && d >= p.Max {
			break
		}
		d *= 2
	}
	if p.Max > 0 && d > p.Max {
		d = p.Max
	}
	return d
}

// RetryAfter returns the remaining lock time, or 0 when unlocked.
func RetryAfter(lockedUntil *time.Time, now time.Time) time.Duration {
	if lockedUntil == nil || !lockedUntil.After(now) {
		return 0
	}
	return lockedUntil.Sub(now)
}

// RetryAfterSeconds rounds up so clients never retry too early.
func RetryAfterSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	s := int(d / time.Second)
	if d%time.Second != 0 {
		s++
	}
	return s
}
