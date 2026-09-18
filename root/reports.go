package root

import (
	"io"
	"net/http"
	"strconv"
	"time"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	reportcmd "github.com/0xsj/overwatch-backend/internal/report/app/command"
	reportdomain "github.com/0xsj/overwatch-backend/internal/report/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type reportResponse struct {
	ReportID    string `json:"report_id"`
	WorkspaceID string `json:"workspace_id"`
	TargetID    string `json:"target_id"`

	Title      string `json:"title"`
	PreparedBy string `json:"prepared_by,omitempty"`

	PeriodStart string `json:"period_start,omitempty"`
	PeriodEnd   string `json:"period_end,omitempty"`

	// Sections is ALWAYS the full seven, with `enabled` on each — never only the
	// enabled ones. A screen has to draw the toggles that are off, and the two
	// that ship off are the ones it most needs to draw.
	Sections []sectionResponse `json:"sections"`

	Revisions int    `json:"revisions"`
	CreatedAt string `json:"created_at"`
}

type sectionResponse struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Enabled bool   `json:"enabled"`

	// Number is the position IN THE ENABLED SET, and is absent when disabled —
	// disable the second section and the rest renumber. A stored number would be
	// wrong the moment a toggle moved.
	Number int `json:"number,omitempty"`

	// Mandatory is the one section that cannot be turned off. A client rendering
	// a disabled toggle for it would offer something the server refuses.
	Mandatory bool `json:"mandatory"`

	// Warning is what turning this off COSTS, and it is server-side because it
	// is an argument about the record rather than a label: a copy of that
	// sentence in the other repository would drift from this one.
	Warning string `json:"warning,omitempty"`

	// Withheld marks the two sections that carry a capability a `client` is not
	// given — `CLAUDE.md`'s "generate a report vs receive its artifacts".
	Withheld bool `json:"withheld"`
}

type revisionResponse struct {
	RevisionID string `json:"revision_id"`
	Number     int    `json:"number"`

	// Hash is the whole point: what the client received is byte-recoverable and
	// hash-citable, and cannot drift under the person holding it.
	Hash     string   `json:"hash"`
	Bytes    int64    `json:"bytes"`
	Sections []string `json:"sections"`
	IssuedAt string   `json:"issued_at"`
	IssuedBy string   `json:"issued_by"`
}

type reportDetailResponse struct {
	reportResponse
	Revisions []revisionResponse `json:"revision_list"`
}

func asReport(r reportdomain.Report) reportResponse {
	out := reportResponse{
		ReportID: r.ID.String(), WorkspaceID: r.WorkspaceID.String(),
		TargetID: r.TargetID.String(), Title: r.Title, PreparedBy: r.PreparedBy,
		Revisions: r.Revisions,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano),
		Sections:  make([]sectionResponse, 0, len(reportdomain.All)),
	}
	if !r.PeriodStart.IsZero() {
		out.PeriodStart = r.PeriodStart.UTC().Format("2006-01-02")
	}
	if !r.PeriodEnd.IsZero() {
		out.PeriodEnd = r.PeriodEnd.UTC().Format("2006-01-02")
	}
	number := 0
	for _, s := range reportdomain.All {
		on := r.On(s)
		one := sectionResponse{
			Key: s.String(), Title: s.Title(), Enabled: on,
			Mandatory: s.Mandatory(), Warning: s.Warning(), Withheld: s.Withheld(),
		}
		if on {
			number++
			one.Number = number
		}
		out.Sections = append(out.Sections, one)
	}
	return out
}

func asRevision(r reportdomain.Revision) revisionResponse {
	names := make([]string, 0, len(r.Sections))
	for _, s := range r.Sections {
		names = append(names, s.String())
	}
	return revisionResponse{
		RevisionID: r.ID.String(), Number: r.Number, Hash: r.Hash, Bytes: r.Bytes,
		Sections: names, IssuedBy: r.IssuedBy.String(),
		IssuedAt: r.IssuedAt.UTC().Format(time.RFC3339Nano),
	}
}

type openReportRequest struct {
	TargetID    string `json:"target_id"`
	Title       string `json:"title"`
	PreparedBy  string `json:"prepared_by"`
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
}

// openReport needs `write`. Opening one is configuring a deliverable rather than
// reading a record, and a `client` — whose ceiling is `read` — therefore cannot,
// which is the ladder doing the work rather than the role.
func (m *me) openReport(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onDeliverable(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in openReportRequest
	if !decodeBody(w, r, &in) {
		return
	}
	target, err := id.Parse(in.TargetID)
	if err != nil {
		httpx.Fail(m.log, w, r, reportdomain.ErrIDRequired)
		return
	}
	from, err := onlyDate(in.PeriodStart)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	to, err := onlyDate(in.PeriodEnd)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	opened, err := m.reportsCmd.Open(r.Context(), workspace, caller, reportcmd.Draft{
		TargetID: target, Title: in.Title, PreparedBy: in.PreparedBy,
		PeriodStart: from, PeriodEnd: to,
	})
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asReport(opened))
}

// onlyDate parses the two period fields. A DATE and not an instant: a report
// covering "Q3" is a claim about a contract, and admitting a time would let a
// timezone move somebody's quarter by a day.
func onlyDate(in string) (time.Time, error) {
	if in == "" {
		return time.Time{}, nil
	}
	at, err := time.Parse("2006-01-02", in)
	if err != nil {
		return time.Time{}, reportdomain.ErrPeriodBackwards
	}
	return at, nil
}

func (m *me) listReports(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onDeliverable(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	var target id.ID
	if raw := r.URL.Query().Get("target"); raw != "" {
		parsed, err := id.Parse(raw)
		if err != nil {
			httpx.WriteJSON(w, r, http.StatusOK, []reportResponse{})
			return
		}
		target = parsed
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	found, err := m.reports.List(r.Context(), workspace, target, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]reportResponse, 0, len(found))
	for _, one := range found {
		out = append(out, asReport(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

func (m *me) readReport(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onDeliverable(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("report"))
	if err != nil {
		httpx.Fail(m.log, w, r, reportdomain.ErrNotFound)
		return
	}
	detail, err := m.reports.Detail(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := reportDetailResponse{
		reportResponse: asReport(detail.Report),
		Revisions:      make([]revisionResponse, 0, len(detail.Revisions)),
	}
	for _, one := range detail.Revisions {
		out.Revisions = append(out.Revisions, asRevision(one))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

type toggleSectionRequest struct {
	Enabled bool `json:"enabled"`
}

func (m *me) toggleSection(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onDeliverable(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("report"))
	if err != nil {
		httpx.Fail(m.log, w, r, reportdomain.ErrNotFound)
		return
	}
	section, err := reportdomain.ParseSection(r.PathValue("section"))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	var in toggleSectionRequest
	if !decodeBody(w, r, &in) {
		return
	}
	next, err := m.reportsCmd.Toggle(r.Context(), workspace, want, section, in.Enabled)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asReport(next))
}

// previewReport renders exactly what would be frozen, WITHOUT freezing it. It is
// the same function `issueReport` uses, deliberately: two implementations of one
// render drift, and the one that drifts is the preview.
func (m *me) previewReport(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onDeliverable(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("report"))
	if err != nil {
		httpx.Fail(m.log, w, r, reportdomain.ErrNotFound)
		return
	}
	doc, err := m.reportsCmd.Render(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, doc)
}

// issueReport FREEZES. It needs `write` — a client may generate a report, and
// `0019` calls that "a write-shaped act" — and it is the one operation here that
// cannot be undone.
func (m *me) issueReport(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onDeliverable(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("report"))
	if err != nil {
		httpx.Fail(m.log, w, r, reportdomain.ErrNotFound)
		return
	}
	revision, err := m.reportsCmd.Issue(r.Context(), workspace, want, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asRevision(revision))
}

// readRevision streams the FROZEN BYTES.
//
// The row is read first and that is the tenancy check — reaching `pkg/blob` with
// a hash alone would let anybody who guessed a content address read another
// client's report, and a content address is exactly the kind of thing that ends
// up in a log.
func (m *me) readRevision(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onDeliverable(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	want, err := id.Parse(r.PathValue("revision"))
	if err != nil {
		httpx.Fail(m.log, w, r, reportdomain.ErrRevisionNotFound)
		return
	}
	found, body, err := m.reports.Open(r.Context(), workspace, want)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", found.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(found.Bytes, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The HASH in a header, so a reader can check the bytes they got against
	// what the record says they were sent without parsing the body.
	w.Header().Set("X-Report-Hash", found.Hash)
	if _, err := io.Copy(w, body); err != nil {
		// The status is already sent, so this cannot become an error response.
		// Logging it is the only honest thing left.
		m.log.ErrorContext(r.Context(), "report stream cut short",
			"revision", found.ID.String(), "cause", err)
	}
}
