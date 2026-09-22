package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestAlertDeliveryNormalizesKindsAndHonorsOptIn(t *testing.T) {
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	preference, err := domain.NewAlertDelivery(
		id.ID{1}, id.ID{2}, true,
		[]string{" record_gap ", "capture_changed", "record_gap"}, at,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := preference.Kinds, []string{"capture_changed", "record_gap"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("kinds=%v, want %v", got, want)
	}
	if !preference.Allows("record_gap") || preference.Allows("watch_failed") {
		t.Fatalf("filtered preference allowed the wrong alert kind")
	}

	all, err := domain.NewAlertDelivery(id.ID{1}, id.ID{2}, true, nil, at)
	if err != nil || !all.Allows("watch_failed") {
		t.Fatalf("empty kinds should mean all alert kinds: %+v err=%v", all, err)
	}
	disabled, err := domain.NewAlertDelivery(id.ID{1}, id.ID{2}, false, nil, at)
	if err != nil || disabled.Allows("watch_failed") {
		t.Fatalf("disabled preference allowed delivery: %+v err=%v", disabled, err)
	}
}

func TestAlertDeliveryRejectsUnknownKind(t *testing.T) {
	if _, err := domain.NewAlertDelivery(id.ID{1}, id.ID{2}, true, []string{"unknown"}, time.Now()); err == nil {
		t.Fatal("unknown alert kind accepted")
	}
}
