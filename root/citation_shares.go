package root

import (
	"net/http"
	"time"

	obsdomain "github.com/0xsj/overwatch-backend/internal/observation/domain"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	sourcequery "github.com/0xsj/overwatch-backend/internal/source/app/query"
	sourcedomain "github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
)

type citationShareResponse struct {
	ShareID       string `json:"share_id"`
	WorkspaceID   string `json:"workspace_id"`
	SourceID      string `json:"source_id"`
	ObservationID string `json:"observation_id"`
	CreatedBy     string `json:"created_by"`
	CreatedAt     string `json:"created_at"`
	RevokedAt     string `json:"revoked_at,omitempty"`
	RevokedBy     string `json:"revoked_by,omitempty"`
	Token         string `json:"token,omitempty"`
}

// citationContextResponse is intentionally a projection. It contains the
// evidence needed to understand one citation, but no internal identifiers or
// retained source bytes that could turn a share into a source export.
type citationContextResponse struct {
	Visibility     string   `json:"visibility"`
	Redactions     []string `json:"redactions"`
	SourceTitle    string   `json:"source_title"`
	SourceOrigin   string   `json:"source_origin"`
	SourceURL      string   `json:"source_url,omitempty"`
	CaptureVersion int      `json:"capture_version"`
	CapturedAt     string   `json:"captured_at"`
	MediaType      string   `json:"media_type"`
	Derived        bool     `json:"derived"`
	Statement      string   `json:"statement"`
	Quote          string   `json:"quote"`
	Locator        string   `json:"locator,omitempty"`
}

func asCitationShare(share obsdomain.CitationShare, token string) citationShareResponse {
	out := citationShareResponse{
		ShareID: share.ID.String(), WorkspaceID: share.WorkspaceID.String(), SourceID: share.SourceID.String(),
		ObservationID: share.ObservationID.String(), CreatedBy: share.CreatedBy.String(),
		CreatedAt: share.CreatedAt.UTC().Format(time.RFC3339Nano), Token: token,
	}
	if share.RevokedAt != nil {
		out.RevokedAt = share.RevokedAt.UTC().Format(time.RFC3339Nano)
	}
	if share.RevokedBy != nil {
		out.RevokedBy = share.RevokedBy.String()
	}
	return out
}

func citationContext(observation obsdomain.Manual, source sourcequery.Detail) (citationContextResponse, error) {
	var capture *sourcedomain.Capture
	for i := range source.Captures {
		if source.Captures[i].ID == observation.CaptureID {
			capture = &source.Captures[i]
			break
		}
	}
	if capture == nil {
		return citationContextResponse{}, sourcedomain.ErrNotFound
	}
	mediaType := capture.MediaType
	derived := observation.ExtractionID != nil
	if derived {
		mediaType = "text/plain"
	}
	return citationContextResponse{
		Visibility:  "recipient",
		Redactions:  []string{"internal_identifiers", "raw_source_bytes", "capture_hash", "author_identity"},
		SourceTitle: source.Source.Title, SourceOrigin: source.Source.Origin, SourceURL: source.Source.URL,
		CaptureVersion: capture.Version, CapturedAt: capture.CapturedAt.UTC().Format(time.RFC3339Nano),
		MediaType: mediaType, Derived: derived, Statement: observation.Statement, Quote: observation.Quote, Locator: observation.Locator,
	}, nil
}

func (m *me) listCitationShares(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	observation, ok := m.sourceID(w, r, "observation")
	if !ok {
		return
	}
	if _, err := m.research.observations.ByID(r.Context(), workspace, source, observation); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	shares, err := m.research.observations.CitationShares(r.Context(), workspace, source, observation)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	out := make([]citationShareResponse, 0, len(shares))
	for _, share := range shares {
		out = append(out, asCitationShare(share, ""))
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

func (m *me) createCitationShare(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	observation, ok := m.sourceID(w, r, "observation")
	if !ok {
		return
	}
	if _, err := m.research.observations.ByID(r.Context(), workspace, source, observation); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	share, token, err := m.research.observationCmd.CreateCitationShare(r.Context(), workspace, source, observation, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, asCitationShare(share, token))
}

func (m *me) revokeCitationShare(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	share, ok := m.sourceID(w, r, "share")
	if !ok {
		return
	}
	revoked, err := m.research.observationCmd.RevokeCitationShare(r.Context(), workspace, share, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, asCitationShare(revoked, ""))
}

func (m *me) readSharedCitation(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onDeliverable(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	digest, valid := handoffShareDigest(r.PathValue("token"))
	if !valid {
		httpx.Fail(m.log, w, r, obsdomain.ErrNotFound)
		return
	}
	share, err := m.research.observations.CitationShareByDigest(r.Context(), workspace, digest)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	observation, err := m.research.observations.ByID(r.Context(), workspace, share.SourceID, share.ObservationID)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	source, err := m.research.sources.Read(r.Context(), workspace, share.SourceID)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	projection, err := citationContext(observation, source)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if err := m.research.observationCmd.RecordCitationShareAccess(r.Context(), workspace, share.SourceID, share.ObservationID, share.ID, caller, "shared"); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, projection)
}
