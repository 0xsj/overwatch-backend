package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestNewMarkerRequiresBothTenantsAndTime(t *testing.T) {
	account := id.ID{1}
	workspace := id.ID{2}
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	if _, err := New(id.Nil, workspace, at); err != ErrIDRequired {
		t.Fatalf("missing account: %v", err)
	}
	if _, err := New(account, id.Nil, at); err != ErrIDRequired {
		t.Fatalf("missing workspace: %v", err)
	}
	if _, err := New(account, workspace, time.Time{}); err != ErrTimeRequired {
		t.Fatalf("missing time: %v", err)
	}
	found, err := New(account, workspace, at)
	if err != nil || found.SeenAt != at {
		t.Fatalf("valid marker: %+v, %v", found, err)
	}
}
