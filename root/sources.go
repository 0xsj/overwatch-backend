package root

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	cleanupapp "github.com/0xsj/overwatch-backend/internal/artifactcleanup/app"
	cleanuppg "github.com/0xsj/overwatch-backend/internal/artifactcleanup/infra/postgres"
	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	assistcmd "github.com/0xsj/overwatch-backend/internal/assistance/app/command"
	assistquery "github.com/0xsj/overwatch-backend/internal/assistance/app/query"
	assistdomain "github.com/0xsj/overwatch-backend/internal/assistance/domain"
	assistpg "github.com/0xsj/overwatch-backend/internal/assistance/infra/postgres"
	briefcmd "github.com/0xsj/overwatch-backend/internal/brief/app/command"
	briefquery "github.com/0xsj/overwatch-backend/internal/brief/app/query"
	briefpg "github.com/0xsj/overwatch-backend/internal/brief/infra/postgres"
	eventcmd "github.com/0xsj/overwatch-backend/internal/event/app/command"
	eventquery "github.com/0xsj/overwatch-backend/internal/event/app/query"
	eventpg "github.com/0xsj/overwatch-backend/internal/event/infra/postgres"
	leadcmd "github.com/0xsj/overwatch-backend/internal/lead/app/command"
	leadquery "github.com/0xsj/overwatch-backend/internal/lead/app/query"
	leadpg "github.com/0xsj/overwatch-backend/internal/lead/infra/postgres"
	obscmd "github.com/0xsj/overwatch-backend/internal/observation/app/command"
	obsquery "github.com/0xsj/overwatch-backend/internal/observation/app/query"
	obspg "github.com/0xsj/overwatch-backend/internal/observation/infra/postgres"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	connectioncmd "github.com/0xsj/overwatch-backend/internal/researchconnection/app/command"
	connectionquery "github.com/0xsj/overwatch-backend/internal/researchconnection/app/query"
	connectionpg "github.com/0xsj/overwatch-backend/internal/researchconnection/infra/postgres"
	recordcmd "github.com/0xsj/overwatch-backend/internal/researchentity/app/command"
	recordquery "github.com/0xsj/overwatch-backend/internal/researchentity/app/query"
	recordpg "github.com/0xsj/overwatch-backend/internal/researchentity/infra/postgres"
	resolutioncmd "github.com/0xsj/overwatch-backend/internal/researchresolution/app/command"
	resolutionquery "github.com/0xsj/overwatch-backend/internal/researchresolution/app/query"
	resolutionpg "github.com/0xsj/overwatch-backend/internal/researchresolution/infra/postgres"
	reviewcmd "github.com/0xsj/overwatch-backend/internal/review/app/command"
	reviewquery "github.com/0xsj/overwatch-backend/internal/review/app/query"
	reviewpg "github.com/0xsj/overwatch-backend/internal/review/infra/postgres"
	sourcecmd "github.com/0xsj/overwatch-backend/internal/source/app/command"
	sourcequery "github.com/0xsj/overwatch-backend/internal/source/app/query"
	sourcedomain "github.com/0xsj/overwatch-backend/internal/source/domain"
	extractioncmd "github.com/0xsj/overwatch-backend/internal/source/extraction/app/command"
	extractionquery "github.com/0xsj/overwatch-backend/internal/source/extraction/app/query"
	extractiondomain "github.com/0xsj/overwatch-backend/internal/source/extraction/domain"
	extractionpg "github.com/0xsj/overwatch-backend/internal/source/extraction/infra/postgres"
	sourcepg "github.com/0xsj/overwatch-backend/internal/source/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/egress"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

// research groups one source-to-citation flow at the composition boundary.
// Sources own retained material; observations own the statements citing it.
type research struct {
	sources        *sourcequery.Sources
	sourceCmd      *sourcecmd.Sources
	observations   *obsquery.ManualObservations
	observationCmd *obscmd.ManualObservations
	relations      *reviewquery.Relations
	relationCmd    *reviewcmd.Relations
	questions      *leadquery.Questions
	questionCmd    *leadcmd.Questions
	assistance     *assistquery.Operations
	assistanceCmd  *assistcmd.Operations
	syntheses      *assistquery.Syntheses
	synthesisCmd   *assistcmd.Syntheses
	events         *eventquery.Events
	eventCmd       *eventcmd.Events
	brief          *briefquery.Briefs
	briefCmd       *briefcmd.Briefs
	records        *recordquery.Records
	recordCmd      *recordcmd.Records
	resolutions    *resolutionquery.Resolutions
	resolutionCmd  *resolutioncmd.Resolutions
	connections    *connectionquery.Connections
	connectionCmd  *connectioncmd.Connections
	extractions    *extractionquery.Extractions
	extractionCmd  *extractioncmd.Extractions
	cleanup        *cleanupapp.Service
	fetcher        referenceFetcher
}

func newResearch(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System) *research {
	return newResearchWithFetcherAndOCR(db, bytes, publisher, ids, clk, egress.New(egress.Config{
		Guard:   egress.NewGuard(egress.Policy{}),
		MaxBody: sourcedomain.MaxBinaryCaptureBytes,
	}), extractioncmd.UnsupportedImageOCR{})
}

func configuredImageOCR(cfg Config) extractioncmd.ImageOCR {
	if strings.TrimSpace(cfg.OCRBinary) == "" {
		return extractioncmd.UnsupportedImageOCR{}
	}
	return extractioncmd.NewProcessImageOCR(extractioncmd.ProcessImageOCRConfig{
		Binary: cfg.OCRBinary, Timeout: cfg.OCRTimeout, MaxOutput: cfg.OCRMaxOutput,
	})
}

type referenceFetcher interface {
	Get(context.Context, string) (*egress.Response, error)
}

func newResearchWithFetcher(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, fetcher referenceFetcher) *research {
	return newResearchWithFetcherAndOCR(db, bytes, publisher, ids, clk, fetcher, extractioncmd.UnsupportedImageOCR{})
}

func newResearchWithOCR(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, ocr extractioncmd.ImageOCR) *research {
	return newResearchWithFetcherAndOCR(db, bytes, publisher, ids, clk, egress.New(egress.Config{
		Guard:   egress.NewGuard(egress.Policy{}),
		MaxBody: sourcedomain.MaxBinaryCaptureBytes,
	}), ocr)
}

func configuredAssistanceProvider(cfg Config) assistapp.Provider {
	if strings.TrimSpace(cfg.AssistanceBinary) == "" {
		return assistapp.LocalSentenceProvider{}
	}
	return assistapp.NewProcessProvider(assistapp.ProcessProviderConfig{
		Binary: cfg.AssistanceBinary, Timeout: cfg.AssistanceTimeout, MaxOutput: cfg.AssistanceMaxOutput,
	})
}

func configuredSynthesisProvider(cfg Config) assistapp.SynthesisProvider {
	if strings.TrimSpace(cfg.SynthesisBinary) == "" {
		return assistapp.LocalSynthesisProvider{}
	}
	return assistapp.NewProcessSynthesisProvider(assistapp.SynthesisProcessProviderConfig{
		Binary: cfg.SynthesisBinary, Timeout: cfg.SynthesisTimeout, MaxOutput: cfg.SynthesisMaxOutput,
	})
}

func newResearchWithOCRAndAssistance(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, ocr extractioncmd.ImageOCR, provider assistapp.Provider) *research {
	return newResearchWithOCRAndAssistanceAndSynthesis(db, bytes, publisher, ids, clk, ocr, provider, assistapp.LocalSynthesisProvider{})
}

func newResearchWithOCRAndAssistanceAndSynthesis(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, ocr extractioncmd.ImageOCR, provider assistapp.Provider, synthesisProvider assistapp.SynthesisProvider) *research {
	return newResearchWithFetcherAndOCRAndAssistanceAndSynthesis(db, bytes, publisher, ids, clk, egress.New(egress.Config{
		Guard:   egress.NewGuard(egress.Policy{}),
		MaxBody: sourcedomain.MaxBinaryCaptureBytes,
	}), ocr, provider, synthesisProvider)
}

func newResearchWithFetcherAndOCR(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, fetcher referenceFetcher, ocr extractioncmd.ImageOCR) *research {
	return newResearchWithFetcherAndOCRAndAssistance(db, bytes, publisher, ids, clk, fetcher, ocr, assistapp.LocalSentenceProvider{})
}

func newResearchWithFetcherAndOCRAndAssistance(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, fetcher referenceFetcher, ocr extractioncmd.ImageOCR, provider assistapp.Provider) *research {
	return newResearchWithFetcherAndOCRAndAssistanceAndSynthesis(db, bytes, publisher, ids, clk, fetcher, ocr, provider, assistapp.LocalSynthesisProvider{})
}

func newResearchWithFetcherAndOCRAndAssistanceAndSynthesis(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, fetcher referenceFetcher, ocr extractioncmd.ImageOCR, provider assistapp.Provider, synthesisProvider assistapp.SynthesisProvider) *research {
	sourceStore := sourcepg.NewStore(db)
	reads := sourcequery.NewSourcesWithClock(sourceStore, bytes, clk)
	observations := obspg.NewStore(db)
	reviewStore := reviewpg.NewStore(db)
	leadStore := leadpg.NewStore(db)
	assistanceStore := assistpg.NewStore(db)
	eventStore := eventpg.NewStore(db)
	briefStore := briefpg.NewStore(db)
	recordStore := recordpg.NewStore(db)
	connectionStore := connectionpg.NewStore(db)
	resolutionStore := resolutionpg.NewStore(db)
	extractionStore := extractionpg.NewStore(db)
	extractionReads := extractionquery.NewExtractions(extractionStore, bytes)
	relations := reviewquery.NewRelations(reviewStore)
	return &research{
		sources:        reads,
		sourceCmd:      sourcecmd.NewSources(sourceStore, bytes, db, publisher, ids, clk),
		observations:   obsquery.NewManualObservations(observations),
		observationCmd: obscmd.NewManualObservations(observations, retainedSources{sources: reads, extractions: extractionReads}, db, publisher, ids, clk),
		relations:      relations,
		relationCmd:    reviewcmd.NewRelations(reviewStore, db, publisher, ids, clk),
		questions:      leadquery.NewQuestions(leadStore),
		questionCmd:    leadcmd.NewQuestions(leadStore, db, publisher, ids, clk),
		assistance:     assistquery.NewOperations(assistanceStore),
		assistanceCmd:  assistcmd.NewOperations(assistanceStore, assistanceCaptures{sources: reads, extractions: extractionReads}, provider, db, publisher, ids, clk),
		syntheses:      assistquery.NewSyntheses(assistanceStore),
		synthesisCmd:   assistcmd.NewSyntheses(assistanceStore, synthesisEvidence{relations: relations}, synthesisProvider, db, publisher, ids, clk),
		events:         eventquery.NewEvents(eventStore),
		eventCmd:       eventcmd.NewEvents(eventStore, db, publisher, ids, clk),
		brief:          briefquery.NewBriefs(briefStore),
		briefCmd:       briefcmd.NewBriefs(briefStore, db, publisher, ids, clk),
		records:        recordquery.NewRecords(recordStore),
		recordCmd:      recordcmd.NewRecords(recordStore, db, publisher, ids, clk),
		resolutions:    resolutionquery.NewResolutions(resolutionStore),
		resolutionCmd:  resolutioncmd.NewResolutions(resolutionStore, recordStore, db, publisher, ids, clk),
		connections:    connectionquery.NewConnections(connectionStore),
		connectionCmd:  connectioncmd.NewConnections(connectionStore, db, publisher, ids, clk),
		extractions:    extractionReads,
		extractionCmd:  extractioncmd.NewExtractionsWithOCR(extractionStore, bytes, extractionCaptures{reads}, db, publisher, ids, clk, ocr),
		cleanup:        cleanupapp.New(cleanuppg.NewStore(db), bytes, publisher, ids, clk),
		fetcher:        fetcher,
	}
}

type synthesisEvidence struct{ relations *reviewquery.Relations }

func (s synthesisEvidence) Evidence(ctx context.Context, workspace, observation id.ID) (assistapp.Observation, error) {
	found, err := s.relations.EvidenceByID(ctx, workspace, observation)
	if err != nil {
		return assistapp.Observation{}, err
	}
	return assistapp.Observation{ID: found.ID, WorkspaceID: found.WorkspaceID, SourceTitle: found.SourceTitle, Statement: found.Statement, Quote: found.Quote}, nil
}

type retainedSources struct {
	sources     *sourcequery.Sources
	extractions *extractionquery.Extractions
}
type assistanceCaptures struct {
	sources     *sourcequery.Sources
	extractions *extractionquery.Extractions
}
type extractionCaptures struct{ sources *sourcequery.Sources }

func (s retainedSources) Retained(ctx context.Context, workspace, source, capture, extraction id.ID) (obscmd.RetainedCapture, error) {
	if extraction.IsZero() {
		held, err := s.sources.Content(ctx, workspace, source, capture)
		if err != nil {
			return obscmd.RetainedCapture{}, err
		}
		return obscmd.RetainedCapture{WorkspaceID: held.WorkspaceID, SourceID: held.SourceID, CaptureID: held.ID, MediaType: held.MediaType, Content: held.Content}, nil
	}
	derived, err := s.extractions.Detail(ctx, workspace, source, capture, extraction)
	if err != nil {
		return obscmd.RetainedCapture{}, err
	}
	if derived.Status != extractiondomain.Succeeded {
		return obscmd.RetainedCapture{}, extractiondomain.ErrUnsupported
	}
	return obscmd.RetainedCapture{WorkspaceID: derived.WorkspaceID, SourceID: derived.SourceID, CaptureID: derived.CaptureID, ExtractionID: derived.ID, MediaType: "text/plain", Content: derived.Text}, nil
}

func (s assistanceCaptures) Retained(ctx context.Context, workspace, source, capture, extraction id.ID) (assistcmd.RetainedCapture, error) {
	if extraction.IsZero() {
		held, err := s.sources.Content(ctx, workspace, source, capture)
		if err != nil {
			return assistcmd.RetainedCapture{}, err
		}
		return assistcmd.RetainedCapture{WorkspaceID: held.WorkspaceID, SourceID: held.SourceID, CaptureID: held.ID, MediaType: held.MediaType, Content: held.Content}, nil
	}
	derived, err := s.extractions.Detail(ctx, workspace, source, capture, extraction)
	if err != nil {
		return assistcmd.RetainedCapture{}, err
	}
	if derived.Status != extractiondomain.Succeeded {
		return assistcmd.RetainedCapture{}, extractiondomain.ErrUnsupported
	}
	return assistcmd.RetainedCapture{WorkspaceID: derived.WorkspaceID, SourceID: derived.SourceID, CaptureID: derived.CaptureID, ExtractionID: derived.ID, MediaType: "text/plain", Content: derived.Text}, nil
}

func (s extractionCaptures) Retained(ctx context.Context, workspace, source, capture id.ID) (extractioncmd.RetainedCapture, error) {
	held, body, err := s.sources.Bytes(ctx, workspace, source, capture)
	if err != nil {
		return extractioncmd.RetainedCapture{}, err
	}
	return extractioncmd.RetainedCapture{WorkspaceID: held.WorkspaceID, SourceID: held.SourceID, CaptureID: held.ID, MediaType: held.MediaType, Bytes: body}, nil
}

func (m *me) registerResearch(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/workspaces/{workspace}/search", m.searchResearch)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources", m.listSources)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/retention-review", m.listRetentionReview)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/retention-cleanup/review", m.readRetentionCleanupReview)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/retention-cleanup/review", m.saveRetentionCleanupReview)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/retention-cleanup/reviews", m.listRetentionCleanupReviews)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/retention-cleanup/review/discard", m.discardRetentionCleanupReview)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/retention-cleanup/status", m.listRetentionCleanupStatus)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/retention-cleanup", m.listRetentionCleanup)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/retention-cleanup", m.sweepRetentionCleanup)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources", m.createSource)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}", m.readSource)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/sources/{source}/retention", m.setSourceRetention)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/sources/{source}/privacy", m.setSourcePrivacy)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/retention-review", m.reviewSourceRetention)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/purge", m.purgeSource)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/captures/{capture}", m.readSourceCapture)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/captures/{capture}/extractions", m.listSourceExtractions)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/captures/{capture}/extract", m.extractSourceCapture)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/captures/{capture}/extractions/{extraction}", m.readSourceExtraction)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/captures", m.captureSource)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/fetch", m.fetchSource)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/observations", m.listSourceObservations)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/observations", m.recordSourceObservation)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/observations/{observation}", m.readSourceObservation)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/questions", m.listQuestions)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/questions", m.createQuestion)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/questions/{question}", m.readQuestion)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/questions/{question}", m.editQuestion)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/events", m.listTimelineEvents)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/events", m.createTimelineEvent)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/events/{event}", m.readTimelineEvent)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/events/{event}", m.editTimelineEvent)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief", m.readWorkingBrief)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/brief", m.saveWorkingBrief)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/snapshots", m.listBriefSnapshots)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/brief/snapshots", m.createBriefSnapshot)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/snapshots/{snapshot}", m.readBriefSnapshot)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/records", m.listResearchRecords)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/records", m.createResearchRecord)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/records/{record}", m.readResearchRecord)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/records/{record}", m.editResearchRecord)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/records/{record}/resolutions", m.listRecordResolutions)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/records/{record}/resolutions", m.createRecordResolution)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/resolutions", m.listResearchResolutions)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/resolutions/{resolution}", m.readResearchResolution)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/resolutions/{resolution}", m.reviewResearchResolution)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/resolutions/{resolution}/reverse", m.reverseResearchResolution)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections", m.listResearchConnections)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/connections", m.createResearchConnection)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections/{connection}/revisions", m.listResearchConnectionRevisions)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections/{connection}/revisions/{revision}", m.readResearchConnectionRevision)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections/{connection}", m.readResearchConnection)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/connections/{connection}", m.editResearchConnection)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/captures/{capture}/assistance", m.generateAssistance)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/captures/{capture}/assistance", m.readLatestAssistance)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/captures/{capture}/assistance/history", m.readAssistanceHistory)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/assistance/{operation}", m.readAssistance)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/assistance/{operation}/proposals/{proposal}", m.reviewAssistanceProposal)
}

func (m *me) sourceID(w http.ResponseWriter, r *http.Request, key string) (id.ID, bool) {
	want, err := id.Parse(r.PathValue(key))
	if err != nil || want.IsZero() {
		httpx.Fail(m.log, w, r, sourcedomain.ErrNotFound)
		return id.ID{}, false
	}
	return want, true
}

func researchPage(w http.ResponseWriter, r *http.Request) (id.ID, int, bool) {
	before := id.ID{}
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = id.Parse(raw)
		if err != nil || before.IsZero() {
			httpx.WriteError(w, r, errors.New(errors.Invalid, "invalid source page cursor"))
			return id.ID{}, 0, false
		}
	}
	size := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			httpx.WriteError(w, r, errors.New(errors.Invalid, "page limit must be between 1 and 100"))
			return id.ID{}, 0, false
		}
		size = n
	}
	return before, size, true
}

// JSON escaping and base64 may expand a binary capture. The envelope allows
// an 8 MiB decoded capture plus transport overhead; the domain enforces the
// decoded content limits separately.
func decodeResearchBody(w http.ResponseWriter, r *http.Request, out any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 12<<20))
	if err != nil {
		httpx.WriteError(w, r, errors.Wrap(err, errors.Invalid, "the request body could not be read"))
		return false
	}
	if !validResearchUnicode(body) {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "source text must contain valid Unicode"))
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		httpx.WriteError(w, r, errors.Wrap(err, errors.Invalid, "the request body could not be read"))
		return false
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "the request must contain one JSON value"))
		return false
	}
	return true
}

func (m *me) listSources(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 200 || !utf8.ValidString(query) {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "source search must be 200 characters or fewer"))
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.sources.List(r.Context(), workspace, before, query, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) searchResearch(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || len(query) > 200 || !utf8.ValidString(query) {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "text search must be between 1 and 200 characters"))
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.sources.Search(r.Context(), workspace, before, query, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) listRetentionReview(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	found, err := m.research.sources.RetentionQueue(r.Context(), workspace, before, size, state)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func cleanupLimit(raw string) (int, bool) {
	if raw == "" {
		return 100, true
	}
	n, err := strconv.Atoi(raw)
	return n, err == nil && n >= 1 && n <= 100
}

func (m *me) listRetentionCleanup(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	limit, ok := cleanupLimit(r.URL.Query().Get("limit"))
	if !ok {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "cleanup limit must be between 1 and 100"))
		return
	}
	found, err := m.research.cleanup.Inventory(r.Context(), workspace, limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) listRetentionCleanupStatus(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	limit, ok := cleanupLimit(r.URL.Query().Get("limit"))
	if !ok {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "cleanup limit must be between 1 and 100"))
		return
	}
	found, err := m.research.cleanup.Status(r.Context(), workspace,
		strings.TrimSpace(r.URL.Query().Get("before")),
		strings.TrimSpace(r.URL.Query().Get("ref")),
		strings.TrimSpace(r.URL.Query().Get("state")), limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

type retentionCleanupRequest struct {
	Confirm      bool     `json:"confirm"`
	Limit        int      `json:"limit"`
	SelectedRefs []string `json:"selected_refs,omitempty"`
	ReviewID     string   `json:"review_id,omitempty"`
}

type retentionCleanupReviewRequest struct {
	SelectedRefs []string `json:"selected_refs"`
}

type retentionCleanupReviewDiscardRequest struct {
	ReviewID string `json:"review_id"`
	Reason   string `json:"reason"`
}

func (m *me) readRetentionCleanupReview(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	found, err := m.research.cleanup.Review(r.Context(), workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) saveRetentionCleanupReview(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	var in retentionCleanupReviewRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	found, err := m.research.cleanup.SaveReview(r.Context(), workspace, caller, in.SelectedRefs)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) listRetentionCleanupReviews(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	limit, ok := cleanupLimit(r.URL.Query().Get("limit"))
	if !ok {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "cleanup review limit must be between 1 and 100"))
		return
	}
	found, err := m.research.cleanup.ReviewHistory(r.Context(), workspace, limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) discardRetentionCleanupReview(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	var in retentionCleanupReviewDiscardRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.ReviewID) == "" {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "review_id is required"))
		return
	}
	reviewID, err := id.Parse(strings.TrimSpace(in.ReviewID))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := m.research.cleanup.DiscardReview(r.Context(), workspace, caller, reviewID, in.Reason); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.cleanup.Review(r.Context(), workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) sweepRetentionCleanup(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelAdmin)
	if !ok {
		return
	}
	var in retentionCleanupRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	if !in.Confirm {
		httpx.WriteError(w, r, errors.New(errors.PreconditionRequired, "cleanup requires explicit confirmation"))
		return
	}
	if in.Limit < 0 || in.Limit > 100 {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "cleanup limit must be between 1 and 100"))
		return
	}
	reviewID := id.Nil
	if in.ReviewID != "" {
		parsed, err := id.Parse(in.ReviewID)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		reviewID = parsed
	}
	found, err := m.research.cleanup.Sweep(r.Context(), workspace, caller, in.Limit, in.SelectedRefs, reviewID)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) createSource(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in sourcecmd.Draft
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.sourceCmd.Create(r.Context(), workspace, caller, in)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}
func (m *me) readSource(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	found, err := m.research.sources.Read(r.Context(), workspace, source)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

type sourceRetentionRequest struct {
	RetentionUntil *string `json:"retention_until"`
}

func (m *me) setSourceRetention(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	var in sourceRetentionRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	var until *time.Time
	if in.RetentionUntil != nil {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*in.RetentionUntil))
		if err != nil {
			httpx.Fail(m.log, w, r, sourcedomain.ErrRetentionInvalid)
			return
		}
		until = &parsed
	}
	if err := m.research.sourceCmd.SetRetention(r.Context(), workspace, source, caller, until); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.sources.Read(r.Context(), workspace, source)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

type sourcePrivacyRequest struct {
	Sensitivity     string `json:"sensitivity"`
	LegalHold       bool   `json:"legal_hold"`
	LegalHoldReason string `json:"legal_hold_reason"`
}

func (m *me) setSourcePrivacy(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	var in sourcePrivacyRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	if err := m.research.sourceCmd.SetPrivacy(r.Context(), workspace, source, caller, strings.TrimSpace(in.Sensitivity), in.LegalHold, in.LegalHoldReason); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.sources.Read(r.Context(), workspace, source)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) reviewSourceRetention(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	review, err := m.research.sourceCmd.RetentionReview(r.Context(), workspace, source)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, review)
}

type sourcePurgeRequest struct {
	Confirm bool   `json:"confirm"`
	Reason  string `json:"reason"`
}

func (m *me) purgeSource(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	var in sourcePurgeRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	if !in.Confirm {
		httpx.WriteError(w, r, errors.New(errors.PreconditionRequired, "purge requires explicit confirmation"))
		return
	}
	result, err := m.research.sourceCmd.Purge(r.Context(), workspace, source, caller, in.Reason)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if !result.Purged {
		httpx.WriteJSON(w, r, http.StatusConflict, result)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, result)
}
func (m *me) readSourceCapture(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	capture, ok := m.sourceID(w, r, "capture")
	if !ok {
		return
	}
	found, err := m.research.sources.Content(r.Context(), workspace, source, capture)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) listSourceExtractions(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	capture, ok := m.sourceID(w, r, "capture")
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.extractions.List(r.Context(), workspace, source, capture, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) extractSourceCapture(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	capture, ok := m.sourceID(w, r, "capture")
	if !ok {
		return
	}
	fresh, err := m.research.extractionCmd.Extract(r.Context(), workspace, source, capture, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}

func (m *me) readSourceExtraction(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	capture, ok := m.sourceID(w, r, "capture")
	if !ok {
		return
	}
	extraction, ok := m.sourceID(w, r, "extraction")
	if !ok {
		return
	}
	found, err := m.research.extractions.Detail(r.Context(), workspace, source, capture, extraction)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) captureSource(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	if _, err := m.research.sources.Read(r.Context(), workspace, source); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	var in struct {
		Content      string `json:"content"`
		ContentBytes []byte `json:"content_base64"`
		MediaType    string `json:"media_type"`
	}
	if !decodeResearchBody(w, r, &in) {
		return
	}
	var captured sourcedomain.Capture
	var err error
	if len(in.ContentBytes) > 0 {
		captured, err = m.research.sourceCmd.AddBinaryCapture(r.Context(), workspace, source, caller, in.MediaType, in.ContentBytes)
	} else {
		captured, err = m.research.sourceCmd.AddCapture(r.Context(), workspace, source, caller, in.MediaType, in.Content)
	}
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, captured)
}

func (m *me) fetchSource(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	detail, err := m.research.sources.Read(r.Context(), workspace, source)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if detail.Source.Origin != "reference" || detail.Source.URL == "" {
		httpx.WriteError(w, r, errors.New(errors.Invalid, "only URL references can be fetched"))
		return
	}
	response, err := m.research.fetcher.Get(r.Context(), detail.Source.URL)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if response.Status < http.StatusOK || response.Status >= http.StatusMultipleChoices {
		httpx.WriteError(w, r, errors.Newf(errors.Unavailable, "reference returned HTTP status %d", response.Status))
		return
	}
	mediaType, err := fetchedMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	if response.Truncated {
		httpx.Fail(m.log, w, r, egress.ErrTooLarge)
		return
	}
	var captured sourcedomain.Capture
	if mediaType == "application/pdf" || mediaType == "image/png" || mediaType == "image/jpeg" || mediaType == "image/webp" {
		captured, err = m.research.sourceCmd.AddBinaryCapture(r.Context(), workspace, source, caller, mediaType, response.Body)
	} else {
		captured, err = m.research.sourceCmd.AddCapture(r.Context(), workspace, source, caller, mediaType, string(response.Body))
	}
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, captured)
}

func fetchedMediaType(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "text/plain", nil
	}
	mediaType, _, err := mime.ParseMediaType(raw)
	if err != nil {
		return "", errors.Wrap(err, errors.Unprocessable, "reference returned an invalid content type")
	}
	switch mediaType {
	case "text/plain", "text/html", "application/json", "application/pdf", "image/png", "image/jpeg", "image/webp":
		return mediaType, nil
	default:
		return "", errors.Newf(errors.Unprocessable, "reference returned unsupported content type %q", mediaType)
	}
}
func (m *me) listSourceObservations(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	if _, err := m.research.sources.Read(r.Context(), workspace, source); err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	found, err := m.research.observations.ForSource(r.Context(), workspace, source, before, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}
func (m *me) recordSourceObservation(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	var in obscmd.ManualDraft
	if !decodeResearchBody(w, r, &in) {
		return
	}
	fresh, err := m.research.observationCmd.Record(r.Context(), workspace, source, caller, in)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}

func (m *me) generateAssistance(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	capture, ok := m.sourceID(w, r, "capture")
	if !ok {
		return
	}
	var in struct {
		ExtractionID id.ID `json:"extraction_id,omitempty"`
	}
	if !decodeResearchBody(w, r, &in) {
		return
	}
	operation, proposals, err := m.research.assistanceCmd.Generate(r.Context(), workspace, source, capture, in.ExtractionID, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, assistquery.Detail{Operation: operation, Proposals: proposals})
}

func (m *me) readLatestAssistance(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	capture, ok := m.sourceID(w, r, "capture")
	if !ok {
		return
	}
	extraction := id.Nil
	if raw := strings.TrimSpace(r.URL.Query().Get("extraction_id")); raw != "" {
		var err error
		extraction, err = id.Parse(raw)
		if err != nil {
			httpx.WriteError(w, r, errors.New(errors.Invalid, "invalid extraction id"))
			return
		}
	}
	found, err := m.research.assistance.Latest(r.Context(), workspace, source, capture, extraction)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readAssistanceHistory(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	capture, ok := m.sourceID(w, r, "capture")
	if !ok {
		return
	}
	extraction := id.Nil
	if raw := strings.TrimSpace(r.URL.Query().Get("extraction_id")); raw != "" {
		var err error
		extraction, err = id.Parse(raw)
		if err != nil {
			httpx.WriteError(w, r, errors.New(errors.Invalid, "invalid extraction id"))
			return
		}
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			httpx.WriteError(w, r, errors.New(errors.Invalid, "assistance history limit must be between 1 and 50"))
			return
		}
		limit = parsed
	}
	found, err := m.research.assistance.History(r.Context(), workspace, source, capture, extraction, limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) readAssistance(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	operation, ok := m.sourceID(w, r, "operation")
	if !ok {
		return
	}
	found, err := m.research.assistance.ByID(r.Context(), workspace, operation)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) reviewAssistanceProposal(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	operation, ok := m.sourceID(w, r, "operation")
	if !ok {
		return
	}
	proposal, ok := m.sourceID(w, r, "proposal")
	if !ok {
		return
	}
	owned, err := m.research.assistance.ByID(r.Context(), workspace, operation)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	belongs := false
	for _, one := range owned.Proposals {
		if one.ID == proposal {
			belongs = true
			break
		}
	}
	if !belongs {
		httpx.Fail(m.log, w, r, assistdomain.ErrNotFound)
		return
	}
	var in struct {
		Decision   string `json:"decision"`
		Statement  string `json:"statement"`
		Quote      string `json:"quote"`
		QuoteStart *int   `json:"quote_start,omitempty"`
		Note       string `json:"note"`
	}
	if !decodeResearchBody(w, r, &in) {
		return
	}
	found, err := m.research.assistanceCmd.Review(r.Context(), workspace, proposal, caller, assistdomain.ReviewDecision(in.Decision), in.Statement, in.Quote, in.QuoteStart, in.Note)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

// Citation links resolve one record directly, even when it is beyond the first page.
func (m *me) readSourceObservation(w http.ResponseWriter, r *http.Request) {
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
	found, err := m.research.observations.ByID(r.Context(), workspace, source, observation)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

// encoding/json replaces malformed Unicode with U+FFFD. Research intake must
// reject that input before decoding, rather than call replacement text verbatim.
// Syntax beyond Unicode escapes remains the JSON decoder's responsibility.
func validResearchUnicode(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	inString := false
	for i := 0; i < len(body); i++ {
		if body[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || body[i] != '\\' {
			continue
		}
		i++
		if i >= len(body) {
			return false
		}
		if body[i] != 'u' {
			continue
		}
		if i+4 >= len(body) {
			return false
		}
		unit, err := strconv.ParseUint(string(body[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if unit >= 0xdc00 && unit <= 0xdfff {
			return false
		}
		if unit >= 0xd800 && unit <= 0xdbff {
			if i+6 >= len(body) || body[i+1] != '\\' || body[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(body[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
