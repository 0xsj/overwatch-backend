package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestClusterRequiresMultipleDistinctEvents(t *testing.T) {
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	if _, err := domain.NewCluster(eid(30), eid(31), eid(32), "One event", "", []id.ID{eid(33)}, at); err != domain.ErrClusterEventCount {
		t.Fatalf("one event error=%v, want event count error", err)
	}
	if _, err := domain.NewCluster(eid(30), eid(31), eid(32), "Duplicate", "", []id.ID{eid(33), eid(33)}, at); err != domain.ErrDuplicateClusterEvent {
		t.Fatalf("duplicate event error=%v, want duplicate error", err)
	}
}

func TestClusterReviewRequiresRationaleAndRecordsReviewer(t *testing.T) {
	createdAt := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	reviewedAt := createdAt.Add(time.Hour)
	cluster, err := domain.NewCluster(eid(34), eid(35), eid(36), "Possible same occurrence", "Review two authored events together.", []id.ID{eid(37), eid(38)}, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cluster.Review(eid(39), domain.ClusterAccepted, "", reviewedAt); err != domain.ErrClusterReviewRequired {
		t.Fatalf("empty accepted rationale error=%v, want review required", err)
	}
	cluster, err = cluster.Review(eid(39), domain.ClusterAccepted, "The retained observations support one bounded hypothesis.", reviewedAt)
	if err != nil {
		t.Fatal(err)
	}
	if cluster.State != domain.ClusterAccepted || cluster.ReviewedBy == nil || *cluster.ReviewedBy != eid(39) || cluster.ReviewedAt == nil || cluster.ReviewNote == "" {
		t.Fatalf("unexpected reviewed cluster: %+v", cluster)
	}
	cluster, err = cluster.Review(eid(40), domain.ClusterProposed, "", reviewedAt.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if cluster.State != domain.ClusterProposed || cluster.ReviewedBy != nil || cluster.ReviewedAt != nil || cluster.ReviewNote != "" {
		t.Fatalf("proposed cluster retained review metadata: %+v", cluster)
	}
}
