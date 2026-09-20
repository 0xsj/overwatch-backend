package root

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	rundomain "github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// changeResponse is a read projection. It intentionally does not expose the
// observation ids as the identity of a change: the same subject/field can be
// re-extracted and still be one comparison row. The run ids remain so a reader
// can open the evidence that made either side of the comparison.
type changeResponse struct {
	ChangeID      string `json:"change_id"`
	Kind          string `json:"kind"`
	TargetID      string `json:"target_id"`
	TargetName    string `json:"target_name"`
	SubjectKind   string `json:"subject_kind"`
	SubjectValue  string `json:"subject_value"`
	Field         string `json:"field"`
	PreviousValue string `json:"previous_value,omitempty"`
	CurrentValue  string `json:"current_value,omitempty"`
	Summary       string `json:"summary"`
	CurrentRunID  string `json:"current_run_id"`
	PreviousRunID string `json:"previous_run_id"`
	ChangedAt     string `json:"changed_at"`
}

type changesResponse struct {
	Changes     []changeResponse     `json:"changes"`
	Comparisons []comparisonResponse `json:"comparisons,omitempty"`
	SeenAt      string               `json:"seen_at,omitempty"`
}

type comparisonResponse struct {
	TargetID      string `json:"target_id"`
	TargetName    string `json:"target_name"`
	CurrentRunAt  string `json:"current_run_at"`
	PreviousRunAt string `json:"previous_run_at"`
}

type observedValue struct {
	SubjectKind  string
	SubjectValue string
	Field        string
	Value        string
	ObservedAt   time.Time
}

type runSnapshot struct {
	Values   map[string]observedValue
	Subjects map[string]struct{}
}

func newRunSnapshot() runSnapshot {
	return runSnapshot{
		Values:   map[string]observedValue{},
		Subjects: map[string]struct{}{},
	}
}

func observationKey(kind, value, field string) string {
	return kind + "\x00" + value + "\x00" + field
}

func subjectKey(kind, value string) string {
	return kind + "\x00" + value
}

// diffRunSnapshots is deliberately pure. The important rule is the gone side:
// a missing field is only a disappearance when the same subject was measured
// in the newer run. A subject absent from the newer snapshot was not checked,
// and must not be reported as gone.
func diffRunSnapshots(targetID, targetName string, current, previous rundomain.Run,
	now, before runSnapshot) []changeResponse {
	out := make([]changeResponse, 0)
	changedAt := current.FinishedAt
	if changedAt.IsZero() {
		changedAt = current.StartedAt
	}
	for key, value := range now.Values {
		old, ok := before.Values[key]
		if !ok {
			kind := "added"
			summary := "new subject observed"
			if _, existed := before.Subjects[subjectKey(value.SubjectKind, value.SubjectValue)]; existed {
				summary = value.Field + " answered for the first time"
			}
			out = append(out, makeChange(targetID, targetName, kind, value, observedValue{},
				current, previous, summary, changedAt))
			continue
		}
		if old.Value != value.Value {
			out = append(out, makeChange(targetID, targetName, "changed", value, old,
				current, previous, value.Field+" changed", changedAt))
		}
	}
	for key, old := range before.Values {
		if _, stillThere := now.Values[key]; stillThere {
			continue
		}
		if _, subjectMeasured := now.Subjects[subjectKey(old.SubjectKind, old.SubjectValue)]; !subjectMeasured {
			continue
		}
		out = append(out, makeChange(targetID, targetName, "gone", observedValue{}, old,
			current, previous, old.Field+" stopped being reported; the subject was measured again", changedAt))
	}
	return out
}

func makeChange(targetID, targetName, kind string, current, previous observedValue,
	newer, older rundomain.Run, summary string, changedAt time.Time) changeResponse {
	value := current
	if value.SubjectValue == "" {
		value = previous
	}
	return changeResponse{
		ChangeID: fmt.Sprintf("%s:%s:%s:%s:%s", newer.ID, kind, value.SubjectKind, value.SubjectValue, value.Field),
		Kind:     kind, TargetID: targetID, TargetName: targetName,
		SubjectKind: value.SubjectKind, SubjectValue: value.SubjectValue, Field: value.Field,
		PreviousValue: previous.Value, CurrentValue: current.Value,
		Summary: summary, CurrentRunID: newer.ID.String(), PreviousRunID: older.ID.String(),
		ChangedAt: changedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (m *me) changes(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	seenAt, err := m.seen.ForAccount(r.Context(), caller, workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	targets, err := m.targets.Live(r.Context(), workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := changesResponse{Changes: make([]changeResponse, 0), Comparisons: make([]comparisonResponse, 0)}
	if !seenAt.IsZero() {
		out.SeenAt = seenAt.UTC().Format(time.RFC3339Nano)
	}
	for _, target := range targets {
		runs, err := m.runs.All(r.Context(), workspace, target.ID)
		if err != nil {
			httpx.Fail(m.log, w, r, err)
			return
		}
		completed := make([]rundomain.Run, 0, len(runs))
		for _, one := range runs {
			if one.State == rundomain.StateComplete {
				completed = append(completed, one)
			}
		}
		sort.SliceStable(completed, func(i, j int) bool {
			return completed[i].StartedAt.After(completed[j].StartedAt)
		})
		if len(completed) < 2 {
			continue
		}
		current, previous := completed[0], completed[1]
		out.Comparisons = append(out.Comparisons, comparisonResponse{
			TargetID: target.ID.String(), TargetName: target.Name,
			CurrentRunAt:  current.StartedAt.UTC().Format(time.RFC3339Nano),
			PreviousRunAt: previous.StartedAt.UTC().Format(time.RFC3339Nano),
		})
		newer, err := m.snapshot(r, workspace, current)
		if err != nil {
			httpx.Fail(m.log, w, r, err)
			return
		}
		older, err := m.snapshot(r, workspace, previous)
		if err != nil {
			httpx.Fail(m.log, w, r, err)
			return
		}
		out.Changes = append(out.Changes, diffRunSnapshots(
			target.ID.String(), target.Name, current, previous, newer, older)...)
	}
	sort.SliceStable(out.Changes, func(i, j int) bool {
		return out.Changes[i].ChangedAt > out.Changes[j].ChangedAt
	})
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

type seenResponse struct {
	SeenAt string `json:"seen_at"`
}

func (m *me) markChangesSeen(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	marker, err := m.seenCmd.Mark(r.Context(), caller, workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, seenResponse{SeenAt: marker.SeenAt.UTC().Format(time.RFC3339Nano)})
}

func (m *me) snapshot(r *http.Request, workspace id.ID, run rundomain.Run) (runSnapshot, error) {
	detail, err := m.runs.Detail(r.Context(), workspace, run.ID)
	if err != nil {
		return runSnapshot{}, err
	}
	out := newRunSnapshot()
	for _, invocation := range detail.Invocations {
		rows, err := m.observed.ForInvocation(r.Context(), workspace, invocation.ID, 1000)
		if err != nil {
			return runSnapshot{}, err
		}
		for _, row := range rows {
			out.Subjects[subjectKey(row.SubjectKind, row.SubjectValue)] = struct{}{}
			key := observationKey(row.SubjectKind, row.SubjectValue, row.Field)
			previous, exists := out.Values[key]
			if !exists || row.ObservedAt.After(previous.ObservedAt) {
				out.Values[key] = observedValue{
					SubjectKind: row.SubjectKind, SubjectValue: row.SubjectValue,
					Field: row.Field, Value: row.Value, ObservedAt: row.ObservedAt,
				}
			}
		}
	}
	return out, nil
}
