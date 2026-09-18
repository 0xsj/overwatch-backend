package postgres_test

import (
	"context"
	"testing"
)

func TestSnapshotDoesNotSeeCommitsBetweenSectionReads(t *testing.T) {
	p := open(t)
	ctx := context.Background()
	exec(t, p, "create table snapshot_count (n int)")
	exec(t, p, "insert into snapshot_count values (1)")
	err := p.InSnapshot(ctx, func(snapshot context.Context) error {
		var first, second int
		if err := p.DB(snapshot).QueryRow(snapshot, "select count(*) from snapshot_count").Scan(&first); err != nil {
			return err
		}
		// This uses the pool, not the snapshot context: it commits on a second
		// connection between two section reads, just as background intake does.
		exec(t, p, "insert into snapshot_count values (2)")
		if err := p.InTx(snapshot, func(joined context.Context) error {
			return p.DB(joined).QueryRow(joined, "select count(*) from snapshot_count").Scan(&second)
		}); err != nil {
			return err
		}
		if first != 1 || second != 1 {
			t.Fatalf("one document saw two database states: %d then %d", first, second)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var after int
	if err := p.DB(ctx).QueryRow(ctx, "select count(*) from snapshot_count").Scan(&after); err != nil || after != 2 {
		t.Fatalf("independent commit was not present after snapshot: %d, %v", after, err)
	}
}

func TestSnapshotRefusesToJoinReadCommitted(t *testing.T) {
	p := open(t)
	called := false
	err := p.InTx(context.Background(), func(ctx context.Context) error {
		return p.InSnapshot(ctx, func(context.Context) error { called = true; return nil })
	})
	if err == nil || called {
		t.Fatalf("snapshot silently weakened to outer transaction: called=%v err=%v", called, err)
	}
}
