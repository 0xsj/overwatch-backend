package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxTitleLength      = 200
	MaxPreparedByLength = 200
)

// Report is a stored CONFIGURATION — decisions/0042. It is not a render and it
// is not a document; it is what a document would be made of, until somebody
// issues one.
//
//	draft    sections toggle · counts render LIVE · nothing is promised
//	issued   a revision exists. The BYTES are the deliverable
//
// **The configuration stays editable after issuing.** The revision is frozen;
// the report is not. The alternative makes a firm clone a report to change one
// toggle, and a pile of near-identical reports is worse than a revision list.
type Report struct {
	ID          id.ID
	WorkspaceID id.ID

	// TargetID is which engagement subject this is about. A report is per
	// target rather than per workspace because "the engagement written up" is
	// one client's estate, and a workspace can hold more than one.
	TargetID id.ID

	Title string

	// PreparedBy is FREE TEXT and not an account id, deliberately. It is the
	// name that appears on the document — a firm's name, a partner's — and it
	// is not who pressed the button. Who pressed it is `issued_by` on the
	// revision, which is a record and never a byline.
	PreparedBy string

	// The engagement window the document covers. Both are dates a person types:
	// nothing here derives them from the first and last run, because a report
	// covering "Q3" is a claim about a contract rather than about the data.
	PeriodStart time.Time
	PeriodEnd   time.Time

	// Sections is the ENABLED SET, stored. Defaults are defaults; a report
	// configured a year ago with the invocation log on must still say so after
	// the default moves — decisions/0042 §2.
	Sections map[Section]bool

	// Revisions counts what has been issued. Zero means this has never left the
	// building, which is a different fact from "issued and then edited".
	Revisions int

	CreatedBy id.ID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// New builds a report with the DEFAULT section set, which is five on and two
// off.
func New(newID, workspace, target, by id.ID, title, preparedBy string,
	from, to time.Time, at time.Time) (Report, error) {
	if newID.IsZero() || target.IsZero() || by.IsZero() {
		return Report{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Report{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Report{}, ErrTimeRequired
	}
	title = strings.TrimSpace(title)
	if title == "" || len(title) > MaxTitleLength {
		return Report{}, ErrTitleRequired
	}
	preparedBy = strings.TrimSpace(preparedBy)
	if len(preparedBy) > MaxPreparedByLength {
		return Report{}, ErrPreparedByTooLong
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return Report{}, ErrPeriodBackwards
	}
	sections := make(map[Section]bool, len(All))
	for _, s := range All {
		sections[s] = s.DefaultOn()
	}
	return Report{
		ID: newID, WorkspaceID: workspace, TargetID: target,
		Title: title, PreparedBy: preparedBy,
		PeriodStart: from, PeriodEnd: to,
		Sections: sections, CreatedBy: by, CreatedAt: at, UpdatedAt: at,
	}, nil
}

// Toggle turns one section on or off, and REFUSES to turn off the one that
// cannot be — see [Section.Mandatory].
func (r Report) Toggle(s Section, on bool, at time.Time) (Report, error) {
	if at.IsZero() {
		return r, ErrTimeRequired
	}
	if !on && s.Mandatory() {
		return r, ErrSectionMandatory
	}
	next := r
	// COPIED, not shared. A map is a reference and every other constructor here
	// returns a value the caller may keep — one that aliased its receiver's
	// sections would edit a report somebody else is holding.
	next.Sections = make(map[Section]bool, len(r.Sections))
	for k, v := range r.Sections {
		next.Sections[k] = v
	}
	next.Sections[s] = on
	next.UpdatedAt = at
	return next, nil
}

// Enabled is the enabled set IN CANONICAL ORDER, which is what the numbering is
// derived from. A section missing from the map reads as its default rather than
// as off: a row written before a section existed must not silently disable it.
func (r Report) Enabled() []Section {
	out := make([]Section, 0, len(All))
	for _, s := range All {
		on, held := r.Sections[s]
		if !held {
			on = s.DefaultOn()
		}
		if on {
			out = append(out, s)
		}
	}
	return out
}

// On answers one section, with the same default-if-absent rule.
func (r Report) On(s Section) bool {
	if on, held := r.Sections[s]; held {
		return on
	}
	return s.DefaultOn()
}

// Issued records that a revision now exists. It does NOT freeze the report —
// the revision is the frozen thing.
func (r Report) Issued(at time.Time) (Report, error) {
	if at.IsZero() {
		return r, ErrTimeRequired
	}
	next := r
	next.Revisions = r.Revisions + 1
	next.UpdatedAt = at
	return next, nil
}

// Revision is one ISSUED document: the bytes, and the hash of the bytes.
//
// **It is the same shape an artifact has, for the same reason** — a claim in
// this report can be re-derived, and the document cannot drift under the person
// holding it. `pkg/blob` holds the bytes and has no delete.
type Revision struct {
	ID       id.ID
	ReportID id.ID
	// WorkspaceID is carried so a revision can be read without first reading
	// its report, which is the whole of a client's access path.
	WorkspaceID id.ID

	// Number counts from 1 within a report. A report sent in January and
	// re-sent in March is two documents, and a dispute about a number in the
	// January one is answerable.
	Number int

	Hash      string
	Bytes     int64
	MediaType string

	// Sections is what this revision CONTAINED, in order — a copy of the
	// enabled set at the moment it was issued, because the report's own set may
	// have moved since and "what a given client received is a fact about that
	// document".
	Sections []Section

	IssuedBy id.ID
	IssuedAt time.Time
}

func NewRevision(newID, report, workspace, by id.ID, number int,
	hash string, bytes int64, sections []Section, at time.Time) (Revision, error) {
	if newID.IsZero() || report.IsZero() || by.IsZero() {
		return Revision{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Revision{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Revision{}, ErrTimeRequired
	}
	if number < 1 {
		return Revision{}, ErrRevisionNumber
	}
	if strings.TrimSpace(hash) == "" {
		// A revision this system cannot address is not a deliverable — the same
		// rule `0003` applies to an edge and `0041` to a finding.
		return Revision{}, ErrHashRequired
	}
	if len(sections) == 0 {
		return Revision{}, ErrNoSections
	}
	held := make([]Section, len(sections))
	copy(held, sections)
	return Revision{
		ID: newID, ReportID: report, WorkspaceID: workspace, Number: number,
		Hash: hash, Bytes: bytes, MediaType: "application/json",
		Sections: held, IssuedBy: by, IssuedAt: at,
	}, nil
}

const (
	EventReportOpened = "report.opened"
	EventReportIssued = "report.issued"

	SubjectKind = "workspace"
)

// Issued is a DECISION — 0014. Somebody chose to send a client a document about
// their own estate, and that has an author, no outcome column, and a reason to
// outlive the journal's retention.
type IssuedEvent struct {
	WorkspaceID string   `json:"workspace_id"`
	ReportID    string   `json:"report_id"`
	Revision    int      `json:"revision"`
	Hash        string   `json:"hash"`
	Sections    []string `json:"sections"`
	// Withheld names which of the two capability sections were LEFT OUT. It is
	// in the envelope because a subscriber cannot compute it — 0013 — and "we
	// sent a report with no scope proof" is the sentence an audit reader wants.
	Withheld []string `json:"withheld"`
}
