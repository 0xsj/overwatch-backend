package postgres

import (
	"context"

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

	tx, err := p.pool.Begin(ctx)
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
