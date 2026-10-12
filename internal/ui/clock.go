package ui

import (
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Clock is the time source routes use to pace polling and to measure how long
// something has been running. It exists so behaviour is asserted by advancing a
// clock rather than by sleeping through a test: a route asks the clock for a
// channel and the test decides when it fires.
type Clock interface {
	// After reports a channel that receives once d has elapsed.
	After(d time.Duration) <-chan time.Time
	// Now is the current instant.
	Now() time.Time
}

// RealClock is the production Clock.
type RealClock struct{}

// After reports a channel that fires after d.
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Now reports the current time.
func (RealClock) Now() time.Time { return time.Now() }

// pollMsg asks the route that scheduled it to refresh.
//
// The token identifies the scheduling session. A tick that arrives after the
// route moved to a tab that does not poll, or after the route was replaced,
// carries a token nobody recognises and is dropped instead of starting a fetch
// no screen is waiting for.
type pollMsg struct{ token int }

// pollTokens issues session tokens. A counter is enough: the tokens only need
// to tell this process's sessions apart.
var pollTokens atomic.Int64

// nextPollToken returns a token no live session holds.
func nextPollToken() int { return int(pollTokens.Add(1)) }

// pollTick returns a Cmd that delivers a poll tick for token after interval.
//
// It is a Cmd rather than a timer owned by the route so the tick arrives on the
// update loop, which keeps a route a value with no goroutine of its own. A
// non-positive interval disables polling, which is how a test or an operator
// turns it off.
func pollTick(c Clock, interval time.Duration, token int) tea.Cmd {
	if interval <= 0 {
		return nil
	}
	if c == nil {
		c = RealClock{}
	}
	return func() tea.Msg {
		<-c.After(interval)
		return pollMsg{token: token}
	}
}
