package domain

import "github.com/0xsj/overwatch-backend/pkg/id"

type ClusterCoverageState string

const (
	ClusterNoEvidence         ClusterCoverageState = "no_evidence"
	ClusterNeedsCorroboration ClusterCoverageState = "needs_corroboration"
	ClusterContradiction      ClusterCoverageState = "contradiction_found"
	ClusterUnresolved         ClusterCoverageState = "unresolved"
	ClusterReviewIncomplete   ClusterCoverageState = "review_incomplete"
	ClusterCovered            ClusterCoverageState = "covered"
)

// ClusterCoverage is a read projection over all pair decisions whose two
// observations belong to one authored cluster. It describes review coverage,
// not truth, identity, or source independence.
type ClusterCoverage struct {
	ClusterID                id.ID                `json:"cluster_id"`
	ObservationCount         int                  `json:"observation_count"`
	DistinctSourceCount      int                  `json:"distinct_source_count"`
	ReviewedObservationCount int                  `json:"reviewed_observation_count"`
	SupportingCount          int                  `json:"supporting_count"`
	ContradictingCount       int                  `json:"contradicting_count"`
	RepeatingCount           int                  `json:"repeating_count"`
	UnresolvedCount          int                  `json:"unresolved_count"`
	InternalReviewedPairs    int                  `json:"internal_reviewed_pairs"`
	PossibleInternalPairs    int                  `json:"possible_internal_pairs"`
	UnreviewedInternalPairs  int                  `json:"unreviewed_internal_pairs"`
	Status                   ClusterCoverageState `json:"status"`
}
