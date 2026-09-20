package root

import (
	"fmt"
	"net/http"

	leaddomain "github.com/0xsj/overwatch-backend/internal/lead/domain"
	recorddomain "github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	reviewdomain "github.com/0xsj/overwatch-backend/internal/review/domain"
	sourcecmd "github.com/0xsj/overwatch-backend/internal/source/app/command"
	sourcedomain "github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type sourceGapAlertsResponse struct {
	ActiveGapCount int `json:"active_gap_count"`
}

// refreshSourceGapAlerts is an explicit materialization boundary. The read
// projections remain the source of truth; this command simply snapshots the
// currently unresolved question, record, and evidence-cluster gaps into the
// durable alert inbox. It is intentionally callable by a deployment worker or
// a researcher, rather than hiding writes inside GET /source-alerts.
func (m *me) refreshSourceGapAlerts(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		return
	}
	gaps, err := m.currentDerivedGapAlerts(r, workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	count, err := m.research.sourceCmd.SyncDerivedGapAlerts(r.Context(), workspace, caller, gaps)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, sourceGapAlertsResponse{ActiveGapCount: count})
}

func (m *me) currentDerivedGapAlerts(r *http.Request, workspace id.ID) ([]sourcecmd.DerivedGapAlert, error) {
	questions := make([]leaddomain.Question, 0)
	var questionBefore id.ID
	for {
		page, err := m.research.questions.Filtered(r.Context(), workspace, questionBefore, leaddomain.Open.String(), 100)
		if err != nil {
			return nil, err
		}
		questions = append(questions, page.Items...)
		if page.NextCursor == nil {
			break
		}
		questionBefore = *page.NextCursor
	}

	relations := make([]reviewdomain.Relation, 0)
	var relationBefore id.ID
	for {
		page, err := m.research.relations.Relations(r.Context(), workspace, relationBefore, 100)
		if err != nil {
			return nil, err
		}
		relations = append(relations, page.Items...)
		if page.NextCursor == nil {
			break
		}
		relationBefore = *page.NextCursor
	}

	records := make([]recorddomain.Record, 0)
	var recordBefore id.ID
	for {
		page, err := m.research.records.List(r.Context(), workspace, recordBefore, "", recorddomain.Kind(""), recorddomain.CitationAny, recorddomain.ResolutionAny, 100)
		if err != nil {
			return nil, err
		}
		records = append(records, page.Items...)
		if page.NextCursor == nil {
			break
		}
		recordBefore = *page.NextCursor
	}

	clusters := make([]reviewdomain.Cluster, 0)
	var clusterBefore id.ID
	for {
		page, err := m.research.clusters.List(r.Context(), workspace, clusterBefore, 100)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, page.Items...)
		if page.NextCursor == nil {
			break
		}
		clusterBefore = *page.NextCursor
	}

	coverage := make(map[id.ID]reviewdomain.ClusterCoverage, len(clusters))
	var coverageBefore id.ID
	for {
		page, err := m.research.clusterCoverage.List(r.Context(), workspace, coverageBefore, 100)
		if err != nil {
			return nil, err
		}
		for _, one := range page.Items {
			coverage[one.ClusterID] = one
		}
		if page.NextCursor == nil {
			break
		}
		coverageBefore = *page.NextCursor
	}

	out := make([]sourcecmd.DerivedGapAlert, 0)
	for _, question := range questions {
		status, compared, unresolved, contradicting := questionGapStatus(question, relations)
		if status == "reviewed" {
			continue
		}
		title := "Open question needs evidence review"
		switch status {
		case "no_evidence":
			title = "Open question has no cited evidence"
		case "not_compared":
			title = "Open question has not been compared"
		case "conflicted":
			title = "Open question has conflicting evidence"
		}
		detail := fmt.Sprintf("%s · %d cited observation(s), %d compared, %d unresolved relation(s), %d contradiction(s).", question.Prompt, len(question.ObservationIDs), compared, unresolved, contradicting)
		out = append(out, sourcecmd.DerivedGapAlert{Kind: sourcedomain.AlertKindQuestionGap, TargetID: question.ID, DedupeKey: "question-gap:" + question.ID.String() + ":" + status, Title: title, Detail: detail})
	}
	for _, record := range records {
		status, reviewed, unresolved, contradicting, internal, possible := recordGapStatus(record, relations)
		if status == "covered" {
			continue
		}
		out = append(out, sourcecmd.DerivedGapAlert{
			Kind:      sourcedomain.AlertKindRecordGap,
			TargetID:  record.ID,
			DedupeKey: "record-gap:" + record.ID.String() + ":" + status,
			Title:     recordGapTitle(status),
			Detail:    fmt.Sprintf("%s · %d cited observation(s), %d reviewed, %d internal comparison(s) of %d, %d unresolved relation(s), %d contradiction(s).", record.Name, len(record.ObservationIDs), reviewed, internal, possible, unresolved, contradicting),
		})
	}
	for _, cluster := range clusters {
		one, ok := coverage[cluster.ID]
		if !ok || one.Status == reviewdomain.ClusterCovered {
			continue
		}
		status := string(one.Status)
		out = append(out, sourcecmd.DerivedGapAlert{
			Kind:      sourcedomain.AlertKindClusterGap,
			TargetID:  cluster.ID,
			DedupeKey: "cluster-gap:" + cluster.ID.String() + ":" + status,
			Title:     clusterGapTitle(status),
			Detail:    fmt.Sprintf("%s · %d cited observation(s), %d reviewed, %d internal comparison(s) of %d, %d unresolved relation(s), %d contradiction(s).", cluster.Title, one.ObservationCount, one.ReviewedObservationCount, one.InternalReviewedPairs, one.PossibleInternalPairs, one.UnresolvedCount, one.ContradictingCount),
		})
	}
	return out, nil
}

func recordGapStatus(record recorddomain.Record, relations []reviewdomain.Relation) (status string, reviewed, unresolved, contradicting, internal, possible int) {
	observationIDs := make(map[id.ID]struct{}, len(record.ObservationIDs))
	for _, observation := range record.ObservationIDs {
		observationIDs[observation] = struct{}{}
	}
	touched := make(map[id.ID]struct{})
	for _, relation := range relations {
		_, left := observationIDs[relation.LeftObservationID]
		_, right := observationIDs[relation.RightObservationID]
		if !left && !right {
			continue
		}
		if left {
			touched[relation.LeftObservationID] = struct{}{}
		}
		if right {
			touched[relation.RightObservationID] = struct{}{}
		}
		if left && right {
			internal++
		}
		switch relation.Kind {
		case reviewdomain.Unresolved:
			unresolved++
		case reviewdomain.Contradicts:
			contradicting++
		}
	}
	reviewed = len(touched)
	possible = len(record.ObservationIDs) * maxInt(len(record.ObservationIDs)-1, 0) / 2
	status = "covered"
	if len(record.ObservationIDs) == 0 {
		return "no_evidence", reviewed, unresolved, contradicting, internal, possible
	}
	if contradicting > 0 {
		return "contradiction_found", reviewed, unresolved, contradicting, internal, possible
	}
	if len(record.ObservationIDs) < 2 {
		return "needs_corroboration", reviewed, unresolved, contradicting, internal, possible
	}
	if unresolved > 0 {
		return "unresolved", reviewed, unresolved, contradicting, internal, possible
	}
	if internal < possible {
		return "review_incomplete", reviewed, unresolved, contradicting, internal, possible
	}
	return status, reviewed, unresolved, contradicting, internal, possible
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func recordGapTitle(status string) string {
	switch status {
	case "no_evidence":
		return "Record has no cited evidence"
	case "needs_corroboration":
		return "Record needs corroboration"
	case "contradiction_found":
		return "Record has conflicting evidence"
	case "unresolved":
		return "Record has unresolved evidence"
	default:
		return "Record review is incomplete"
	}
}

func clusterGapTitle(status string) string {
	switch status {
	case "no_evidence":
		return "Evidence cluster has no cited evidence"
	case "needs_corroboration":
		return "Evidence cluster needs corroboration"
	case "contradiction_found":
		return "Evidence cluster has conflicting evidence"
	case "unresolved":
		return "Evidence cluster has unresolved evidence"
	default:
		return "Evidence cluster review is incomplete"
	}
}

func questionGapStatus(question leaddomain.Question, relations []reviewdomain.Relation) (status string, compared, unresolved, contradicting int) {
	observationIDs := make(map[id.ID]struct{}, len(question.ObservationIDs))
	for _, observation := range question.ObservationIDs {
		observationIDs[observation] = struct{}{}
	}
	comparedIDs := make(map[id.ID]struct{})
	for _, relation := range relations {
		_, left := observationIDs[relation.LeftObservationID]
		_, right := observationIDs[relation.RightObservationID]
		if !left && !right {
			continue
		}
		if left {
			comparedIDs[relation.LeftObservationID] = struct{}{}
		}
		if right {
			comparedIDs[relation.RightObservationID] = struct{}{}
		}
		switch relation.Kind {
		case reviewdomain.Unresolved:
			unresolved++
		case reviewdomain.Contradicts:
			contradicting++
		}
	}
	compared = len(comparedIDs)
	status = "reviewed"
	if len(question.ObservationIDs) == 0 {
		return "no_evidence", compared, unresolved, contradicting
	}
	if compared == 0 {
		return "not_compared", compared, unresolved, contradicting
	}
	if contradicting > 0 {
		return "conflicted", compared, unresolved, contradicting
	}
	if compared < len(observationIDs) || unresolved > 0 {
		return "partially_compared", compared, unresolved, contradicting
	}
	return status, compared, unresolved, contradicting
}
