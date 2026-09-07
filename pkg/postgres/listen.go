package postgres

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

const (
	listenRetryMin = 250 * time.Millisecond
	listenRetryMax = 30 * time.Second
)

func (p *Pool) Listen(ctx context.Context, channel string, log *slog.Logger) (<-chan struct{}, error) {
	if !schemaName.MatchString(channel) {
		return nil, errors.Newf(errors.Invalid,
			"postgres: %q is not a usable channel name — it is interpolated into LISTEN, "+
				"where a bind parameter is not accepted, so it must match [a-z_][a-z0-9_]{0,62}",
			channel)
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	conn, err := p.listening(ctx, channel)
	if err != nil {
		return nil, err
	}

	wake := make(chan struct{}, 1)
	go func() {
		defer close(wake)
		defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

		for {
			if _, err := conn.WaitForNotification(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.ErrorContext(ctx, "listen dropped", "channel", channel, "cause", err)
				_ = conn.Close(context.WithoutCancel(ctx))

				backoff := listenRetryMin
				for {
					next, err := p.listening(ctx, channel)
					if err == nil {
						conn = next
						log.InfoContext(ctx, "listen resumed", "channel", channel)
						break
					}
					if ctx.Err() != nil {
						return
					}
					log.WarnContext(ctx, "listen not resumed", "channel", channel,
						"retry_in", backoff.String(), "cause", err)
					select {
					case <-ctx.Done():
						return
					case <-time.After(backoff):
					}
					if backoff *= 2; backoff > listenRetryMax {
						backoff = listenRetryMax
					}
				}
				// A gap in listening is a gap in wakes, and the waker cannot know
				// what it missed. One unconditional wake on resume is what turns
				// that into a single late delivery rather than a stalled one.
			}
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}()
	return wake, nil
}

func (p *Pool) listening(ctx context.Context, channel string) (*pgx.Conn, error) {
	conn, err := pgx.ConnectConfig(ctx, p.pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, Translate(ctx, err, "postgres: dial for listen")
	}
	if _, err := conn.Exec(ctx, "listen "+channel); err != nil {
		_ = conn.Close(context.WithoutCancel(ctx))
		return nil, Translate(ctx, err, "postgres: listen "+channel)
	}
	return conn, nil
}
