// Package clock abstracts time for tests. Domain code and platform helpers receive a
// Clock instead of calling time.Now directly.
package clock

import "time"

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// Real returns a Clock that uses time.Now.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Real returns a Clock backed by the system clock.
func Real() Clock { return realClock{} }

// Fake is a Clock that tests control. It is not safe for concurrent use unless
// protected by the test.
type Fake struct {
	now time.Time
}

// NewFake creates a Fake frozen at t.
func NewFake(t time.Time) *Fake { return &Fake{now: t} }

// Now returns the fake current time.
func (f *Fake) Now() time.Time { return f.now }

// Add moves the fake clock forward by d.
func (f *Fake) Add(d time.Duration) { f.now = f.now.Add(d) }
