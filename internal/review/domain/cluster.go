package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	ClusterRecorded    = "review.cluster.recorded"
	ClusterUpdated     = "review.cluster.updated"
	MaxClusterTitle    = 200
	MaxClusterDetails  = 4000
	MaxClusterEvidence = 24
)

type ClusterKind string

const (
	ClaimCluster   ClusterKind = "claim"
	AccountCluster ClusterKind = "account"
)

func (k ClusterKind) String() string { return string(k) }

func ParseClusterKind(raw string) (ClusterKind, error) {
	switch ClusterKind(strings.TrimSpace(raw)) {
	case ClaimCluster, AccountCluster:
		return ClusterKind(strings.TrimSpace(raw)), nil
	default:
		return "", errors.New(errors.Invalid, "cluster kind must be claim or account")
	}
}

// Cluster is an analyst-authored grouping of cited observations. It is a
// review hypothesis, not an identity merge or a claim that the observations
// are independent or true.
type Cluster struct {
	ID             id.ID       `json:"cluster_id"`
	WorkspaceID    id.ID       `json:"workspace_id"`
	Kind           ClusterKind `json:"kind"`
	Title          string      `json:"title"`
	Description    string      `json:"description"`
	ObservationIDs []id.ID     `json:"observation_ids"`
	Author         id.ID       `json:"author"`
	UpdatedBy      id.ID       `json:"updated_by"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

func NewCluster(want, workspace, author id.ID, kind, title, description string, observations []id.ID, at time.Time) (Cluster, error) {
	if want.IsZero() || workspace.IsZero() || author.IsZero() || at.IsZero() {
		return Cluster{}, ErrInvalid
	}
	k, err := ParseClusterKind(kind)
	if err != nil {
		return Cluster{}, err
	}
	title, description, observations, err = validateClusterFields(title, description, observations)
	if err != nil {
		return Cluster{}, err
	}
	return Cluster{ID: want, WorkspaceID: workspace, Kind: k, Title: title, Description: description,
		ObservationIDs: observations, Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at}, nil
}

func (c Cluster) Edit(by id.ID, kind, title, description string, observations []id.ID, at time.Time) (Cluster, error) {
	if c.ID.IsZero() || c.WorkspaceID.IsZero() || by.IsZero() || at.IsZero() {
		return c, ErrInvalid
	}
	k, err := ParseClusterKind(kind)
	if err != nil {
		return c, err
	}
	title, description, observations, err = validateClusterFields(title, description, observations)
	if err != nil {
		return c, err
	}
	next := c
	next.Kind, next.Title, next.Description, next.ObservationIDs = k, title, description, observations
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

func validateClusterFields(title, description string, observations []id.ID) (string, string, []id.ID, error) {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if title == "" || len(title) > MaxClusterTitle || !utf8.ValidString(title) || strings.ContainsRune(title, 0) {
		return "", "", nil, errors.New(errors.Invalid, "cluster title requires UTF-8 text up to 200 bytes")
	}
	if len(description) > MaxClusterDetails || !utf8.ValidString(description) || strings.ContainsRune(description, 0) {
		return "", "", nil, errors.New(errors.Invalid, "cluster description requires UTF-8 text up to 4000 bytes")
	}
	if len(observations) == 0 || len(observations) > MaxClusterEvidence {
		return "", "", nil, errors.New(errors.Invalid, "cluster requires between 1 and 24 observations")
	}
	seen := make(map[id.ID]struct{}, len(observations))
	ordered := make([]id.ID, 0, len(observations))
	for _, observation := range observations {
		if observation.IsZero() {
			return "", "", nil, errors.New(errors.Invalid, "cluster observations must be valid identifiers")
		}
		if _, ok := seen[observation]; ok {
			return "", "", nil, errors.New(errors.Invalid, "cluster observations must be unique")
		}
		seen[observation] = struct{}{}
		ordered = append(ordered, observation)
	}
	return title, description, ordered, nil
}
