package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type txKey struct{}

// DB returns what a query should run on: the transaction in flight if there is
// one, the pool otherwise. A repository calls this and never asks which.
func (p *Pool) DB(ctx context.Context) DBTX {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return p.pool
}

// InTx runs fn inside a transaction. A nested call JOINS the transaction already
// in the context rather than opening a second one — two independent
// transactions against one logical unit of work is how half an operation gets
// committed while the other half rolls back.
//
// Rollback happens on any error and on any panic, and the panic is re-raised
// afterwards: swallowing it turns a bug into a silent no-op.
func (p *Pool) InTx(ctx context.Context, fn func(context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}

	return p.inTx(ctx, pgx.TxOptions{}, fn)
}

// InSnapshot keeps all reads on one repeatable-read snapshot. Joining an outer
// READ COMMITTED transaction would weaken this guarantee, so that is refused.
// An ordinary InTx inside a snapshot still joins it as usual.
func (p *Pool) InSnapshot(ctx context.Context, fn func(context.Context) error) error {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		var isolation string
		if err := tx.QueryRow(ctx, "show transaction_isolation").Scan(&isolation); err != nil {
			return Translate(ctx, err, "postgres: snapshot isolation")
		}
		if isolation != "repeatable read" && isolation != "serializable" {
			return fmt.Errorf("postgres: snapshot requires repeatable read or serializable isolation, got %s", isolation)
		}
		return fn(ctx)
	}
	return p.inTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead}, fn)
}

func (p *Pool) inTx(ctx context.Context, options pgx.TxOptions, fn func(context.Context) error) error {
	tx, err := p.pool.BeginTx(ctx, options)
	if err != nil {
		return Translate(ctx, err, "postgres: begin")
	}

	committed := false
	defer func() {
		if committed {
			return
		}
		// A rollback on an already-finished transaction is not an error worth
		// reporting; the failure that mattered is already on its way up.
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return Translate(ctx, err, "postgres: commit")
	}
	committed = true
	return nil
}
