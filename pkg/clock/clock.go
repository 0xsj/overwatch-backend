package clock

import "time"

type Clock interface {
	Now() time.Time
	Elapsed() time.Duration
}

var (
	_ Clock = System{}
	_ Clock = (*Fake)(nil)
)

func Since(c Clock, t time.Time) time.Duration { return c.Now().Sub(t) }

func Until(c Clock, t time.Time) time.Duration { return t.Sub(c.Now()) }
