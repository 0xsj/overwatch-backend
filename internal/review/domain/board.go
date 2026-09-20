package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// BoardState is a derived triage label for one observation. It describes the
// current review coverage, not whether the cited statement is true.
type BoardState string

const (
	BoardUnreviewed    BoardState = "unreviewed"
	BoardReviewed      BoardState = "reviewed"
	BoardContradiction BoardState = "contradiction"
	BoardUnresolved    BoardState = "unresolved"
)

func (s BoardState) String() string { return string(s) }

func ParseBoardState(raw string) (BoardState, error) {
	switch BoardState(strings.TrimSpace(raw)) {
	case "":
		return "", nil
	case BoardUnreviewed, BoardReviewed, BoardContradiction, BoardUnresolved:
		return BoardState(strings.TrimSpace(raw)), nil
	default:
		return "", errors.New(errors.Invalid, "board state must be unreviewed, reviewed, contradiction, or unresolved")
	}
}

// BoardFilters are workspace-scoped triage filters. Dates apply to the time
// the observation was authored, not to the source's reported event time.
// RecordID and EventID filter through their explicit observation links.
type BoardFilters struct {
	SourceID       id.ID
	RecordID       id.ID
	EventID        id.ID
	Query          string
	State          BoardState
	RecordedFrom   *time.Time
	RecordedTo     *time.Time
	UnresolvedOnly bool
}

func (f BoardFilters) Validate() error {
	if len([]rune(f.Query)) > 200 {
		return errors.New(errors.Invalid, "board search must be 200 characters or fewer")
	}
	if f.RecordedFrom != nil && f.RecordedTo != nil && !f.RecordedFrom.Before(*f.RecordedTo) {
		return errors.New(errors.Invalid, "board date range must have an earlier start")
	}
	_, err := ParseBoardState(f.State.String())
	return err
}

// BoardItem is the investigation-wide observation board projection. The
// relation counts are current pair decisions; cluster_count is authored
// grouping coverage. Neither field implies source independence or truth.
type BoardItem struct {
	Evidence
	ReviewState  BoardState `json:"review_state"`
	Supports     int        `json:"supports"`
	Contradicts  int        `json:"contradicts"`
	Repeats      int        `json:"repeats"`
	Unresolved   int        `json:"unresolved"`
	ClusterCount int        `json:"cluster_count"`
}
