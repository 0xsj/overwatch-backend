package clock

import (
	"context"
	"time"
)

var origin = time.Now()

type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

func (System) Elapsed() time.Duration { return time.Since(origin) }

func (System) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (System) After(d time.Duration) <-chan time.Time { return time.After(d) }
