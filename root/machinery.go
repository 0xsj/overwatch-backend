package root

import (
	"net/http"
	"time"

	healthdomain "github.com/0xsj/overwatch-backend/internal/health/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
)

// This file is `CLAUDE.md`'s `health` NOUN — *"is the MACHINERY well — a tool
// off PATH looks like silence"*.
//
// **It is not `health.go`**, which holds liveness and readiness, and the two
// must not share a file or a name: those answer *"is this PROCESS up"* for an
// orchestrator that will restart it, and this answers *"is anything quietly not
// working"* for a person who will fix it. Confusing them is how a readiness
// probe grows a dependency and turns a database blip into an outage.

// healthResponse answers *"is the machinery well"*, and its shape is an argument.
//
// **`probes` is not diagnostics — it is half the answer.** A clean symptom list
// with no record of what was examined is indistinguishable from a report nobody
// ran, which is `CLAUDE.md`'s rule about counts (`–` never `0`) applied to the
// screen whose entire job is telling silence apart from health.
type healthResponse struct {
	At string `json:"at"`

	// Trustworthy is FALSE when any probe could not run. When it is false,
	// "nothing is wrong" is not a sentence this response supports — render
	// "nothing found in the places we could look", and say which places.
	Trustworthy bool `json:"trustworthy"`

	Probes   []probeResponse   `json:"probes"`
	Symptoms []symptomResponse `json:"symptoms"`
}

type probeResponse struct {
	Kind string `json:"kind"`

	// Measured false means WE COULD NOT LOOK. It is not the same as looking and
	// finding nothing, and a screen that renders both as a tick has become the
	// thing this endpoint exists to detect.
	Measured bool   `json:"measured"`
	Because  string `json:"because,omitempty"`

	// Looked and Found are POINTERS, absent when unmeasured. A zero nothing
	// computed is not a zero.
	Looked *int `json:"looked,omitempty"`
	Found  *int `json:"found,omitempty"`
}

type symptomResponse struct {
	Kind string `json:"kind"`
	// Says is the sentence this silence tells, server-side because it is an
	// argument about what the absence MEANS rather than a label.
	Says string `json:"says"`

	Subject   string `json:"subject"`
	SubjectID string `json:"subject_id,omitempty"`
	Detail    string `json:"detail,omitempty"`
	Count     int    `json:"count"`
	// Since is the OLDEST occurrence — "broken since Tuesday" rather than "last
	// seen a minute ago", which is true of everything still broken.
	Since string `json:"since,omitempty"`
}

// readHealth is a RECORD read: it says what is quietly not working, and a closed
// engagement's machinery is exactly as worth asking about as an open one's.
//
// It is per workspace rather than global because every other read here is, and
// because "is the machinery well" is asked by somebody looking at one client's
// estate wondering why it looks so clean.
func (m *me) readHealth(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	report, err := m.health.Examine(r.Context(), workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}

	out := healthResponse{
		At:          report.At.UTC().Format(time.RFC3339Nano),
		Trustworthy: report.Trustworthy(),
		Probes:      make([]probeResponse, 0, len(report.Probes)),
		Symptoms:    make([]symptomResponse, 0, len(report.Symptoms)),
	}
	for _, p := range report.Probes {
		one := probeResponse{Kind: p.Kind.String(), Measured: p.Measured, Because: p.Because}
		if p.Measured {
			looked, found := p.Looked, p.Found
			one.Looked, one.Found = &looked, &found
		}
		out.Probes = append(out.Probes, one)
	}
	for _, s := range report.Symptoms {
		one := symptomResponse{
			Kind: s.Kind.String(), Says: s.Kind.Says(),
			Subject: s.Subject, Detail: s.Detail, Count: s.Count,
		}
		if !s.SubjectID.IsZero() {
			one.SubjectID = s.SubjectID.String()
		}
		if !s.Since.IsZero() {
			one.Since = s.Since.UTC().Format(time.RFC3339Nano)
		}
		out.Symptoms = append(out.Symptoms, one)
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

var _ = healthdomain.All
