package root

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
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
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
	connectioncmd "github.com/0xsj/overwatch-backend/internal/researchconnection/app/command"
	connectionquery "github.com/0xsj/overwatch-backend/internal/researchconnection/app/query"
	connectiondomain "github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
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
	sources               *sourcequery.Sources
	sourceCmd             *sourcecmd.Sources
	observations          *obsquery.ManualObservations
	observationCmd        *obscmd.ManualObservations
	relations             *reviewquery.Relations
	board                 *reviewquery.Board
	sourceLinks           *reviewquery.SourceLinks
	relationCmd           *reviewcmd.Relations
	sourceLinkCmd         *reviewcmd.SourceLinks
	clusters              *reviewquery.Clusters
	clusterCoverage       *reviewquery.ClusterCoverage
	clusterCmd            *reviewcmd.Clusters
	questions             *leadquery.Questions
	questionCmd           *leadcmd.Questions
	assistance            *assistquery.Operations
	providerRuns          *assistquery.ProviderRuns
	assistanceCmd         *assistcmd.Operations
	providerPolicy        *assistquery.ProviderPolicies
	providerPolicyCmd     *assistcmd.ProviderPolicies
	comparisons           *assistquery.Comparisons
	comparisonCmd         *assistcmd.Comparisons
	questionSuggestions   *assistquery.QuestionSuggestions
	questionSuggestionCmd *assistcmd.QuestionSuggestions
	briefDrafts           *assistquery.BriefDrafts
	briefDraftCmd         *assistcmd.BriefDrafts
	connectionReviews     *assistquery.ConnectionReviews
	connectionReviewCmd   *assistcmd.ConnectionReviews
	syntheses             *assistquery.Syntheses
	synthesisCmd          *assistcmd.Syntheses
	events                *eventquery.Events
	eventCmd              *eventcmd.Events
	eventAccounts         *eventquery.Accounts
	eventAccountCmd       *eventcmd.Accounts
	eventClusters         *eventquery.Clusters
	eventClusterCmd       *eventcmd.Clusters
	eventRelationships    *eventquery.Relationships
	eventRelationshipCmd  *eventcmd.Relationships
	brief                 *briefquery.Briefs
	briefCmd              *briefcmd.Briefs
	records               *recordquery.Records
	recordCmd             *recordcmd.Records
	resolutions           *resolutionquery.Resolutions
	resolutionCmd         *resolutioncmd.Resolutions
	connections           *connectionquery.Connections
	connectionCmd         *connectioncmd.Connections
	extractions           *extractionquery.Extractions
	extractionCmd         *extractioncmd.Extractions
	cleanup               *cleanupapp.Service
	fetcher               referenceFetcher
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
		Binary: cfg.OCRBinary, Args: cfg.OCRArgs, Timeout: cfg.OCRTimeout, MaxOutput: cfg.OCRMaxOutput,
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
		Binary: cfg.AssistanceBinary, Args: cfg.AssistanceArgs, Timeout: cfg.AssistanceTimeout, MaxOutput: cfg.AssistanceMaxOutput,
	})
}

func configuredSynthesisProvider(cfg Config) assistapp.SynthesisProvider {
	if strings.TrimSpace(cfg.SynthesisBinary) == "" {
		return assistapp.LocalSynthesisProvider{}
	}
	return assistapp.NewProcessSynthesisProvider(assistapp.SynthesisProcessProviderConfig{
		Binary: cfg.SynthesisBinary, Args: cfg.SynthesisArgs, Timeout: cfg.SynthesisTimeout, MaxOutput: cfg.SynthesisMaxOutput,
	})
}

func configuredComparisonProvider(cfg Config) assistapp.ComparisonProvider {
	if strings.TrimSpace(cfg.AssistanceBinary) == "" {
		return assistapp.LocalComparisonProvider{}
	}
	return assistapp.NewProcessComparisonProvider(assistapp.ComparisonProcessProviderConfig{
		Binary: cfg.AssistanceBinary, Args: cfg.AssistanceArgs, Timeout: cfg.AssistanceTimeout, MaxOutput: cfg.AssistanceMaxOutput,
	})
}

func configuredQuestionSuggestionProvider(cfg Config) assistapp.QuestionSuggestionProvider {
	if strings.TrimSpace(cfg.AssistanceBinary) == "" {
		return assistapp.LocalQuestionSuggestionProvider{}
	}
	return assistapp.NewProcessQuestionSuggestionProvider(assistapp.QuestionSuggestionsProcessProviderConfig{
		Binary: cfg.AssistanceBinary, Args: cfg.AssistanceArgs, Timeout: cfg.AssistanceTimeout, MaxOutput: cfg.AssistanceMaxOutput,
	})
}

func configuredBriefDraftProvider(cfg Config) assistapp.BriefDraftProvider {
	if strings.TrimSpace(cfg.AssistanceBinary) == "" {
		return assistapp.LocalBriefDraftProvider{}
	}
	return assistapp.NewProcessBriefDraftProvider(assistapp.BriefDraftProcessProviderConfig{
		Binary: cfg.AssistanceBinary, Args: cfg.AssistanceArgs, Timeout: cfg.AssistanceTimeout, MaxOutput: cfg.AssistanceMaxOutput,
	})
}

func configuredConnectionReviewProvider(cfg Config) assistapp.ConnectionReviewProvider {
	if strings.TrimSpace(cfg.AssistanceBinary) == "" {
		return assistapp.LocalConnectionReviewProvider{}
	}
	return assistapp.NewProcessConnectionReviewProvider(assistapp.ConnectionReviewProcessProviderConfig{
		Binary: cfg.AssistanceBinary, Args: cfg.AssistanceArgs, Timeout: cfg.AssistanceTimeout, MaxOutput: cfg.AssistanceMaxOutput,
	})
}

func newResearchWithOCRAndAssistance(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, ocr extractioncmd.ImageOCR, provider assistapp.Provider) *research {
	return newResearchWithOCRAndAssistanceAndSynthesis(db, bytes, publisher, ids, clk, ocr, provider, assistapp.LocalSynthesisProvider{})
}

func newResearchWithOCRAndAssistanceAndSynthesis(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, ocr extractioncmd.ImageOCR, provider assistapp.Provider, synthesisProvider assistapp.SynthesisProvider) *research {
	return newResearchWithFetcherAndOCRAndAssistanceAndSynthesis(db, bytes, publisher, ids, clk, egress.New(egress.Config{
		Guard:   egress.NewGuard(egress.Policy{}),
		MaxBody: sourcedomain.MaxBinaryCaptureBytes,
	}), ocr, provider, assistapp.LocalComparisonProvider{}, assistapp.LocalQuestionSuggestionProvider{}, assistapp.LocalBriefDraftProvider{}, assistapp.LocalConnectionReviewProvider{}, synthesisProvider)
}

func newResearchWithOCRAndAssistanceAndSynthesisAndComparison(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, ocr extractioncmd.ImageOCR, provider assistapp.Provider, comparisonProvider assistapp.ComparisonProvider, questionSuggestionProvider assistapp.QuestionSuggestionProvider, briefDraftProvider assistapp.BriefDraftProvider, connectionReviewProvider assistapp.ConnectionReviewProvider, synthesisProvider assistapp.SynthesisProvider) *research {
	return newResearchWithFetcherAndOCRAndAssistanceAndSynthesis(db, bytes, publisher, ids, clk, egress.New(egress.Config{
		Guard:   egress.NewGuard(egress.Policy{}),
		MaxBody: sourcedomain.MaxBinaryCaptureBytes,
	}), ocr, provider, comparisonProvider, questionSuggestionProvider, briefDraftProvider, connectionReviewProvider, synthesisProvider)
}

func newResearchWithFetcherAndOCR(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, fetcher referenceFetcher, ocr extractioncmd.ImageOCR) *research {
	return newResearchWithFetcherAndOCRAndAssistance(db, bytes, publisher, ids, clk, fetcher, ocr, assistapp.LocalSentenceProvider{})
}

func newResearchWithFetcherAndOCRAndAssistance(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, fetcher referenceFetcher, ocr extractioncmd.ImageOCR, provider assistapp.Provider) *research {
	return newResearchWithFetcherAndOCRAndAssistanceAndSynthesis(db, bytes, publisher, ids, clk, fetcher, ocr, provider, assistapp.LocalComparisonProvider{}, assistapp.LocalQuestionSuggestionProvider{}, assistapp.LocalBriefDraftProvider{}, assistapp.LocalConnectionReviewProvider{}, assistapp.LocalSynthesisProvider{})
}

func newResearchWithFetcherAndOCRAndAssistanceAndSynthesis(db *postgres.Pool, bytes *blob.Store, publisher events.Publisher, ids *id.V7, clk clock.System, fetcher referenceFetcher, ocr extractioncmd.ImageOCR, provider assistapp.Provider, comparisonProvider assistapp.ComparisonProvider, questionSuggestionProvider assistapp.QuestionSuggestionProvider, briefDraftProvider assistapp.BriefDraftProvider, connectionReviewProvider assistapp.ConnectionReviewProvider, synthesisProvider assistapp.SynthesisProvider) *research {
	sourceStore := sourcepg.NewStore(db)
	reads := sourcequery.NewSourcesWithClock(sourceStore, bytes, clk)
	observations := obspg.NewStore(db)
	reviewStore := reviewpg.NewStore(db)
	leadStore := leadpg.NewStore(db)
	assistanceStore := assistpg.NewStore(db)
	eventStore := eventpg.NewStore(db)
	briefStore := briefpg.NewStore(db)
	briefReads := briefquery.NewBriefs(briefStore)
	recordStore := recordpg.NewStore(db)
	connectionStore := connectionpg.NewStore(db)
	connections := connectionquery.NewConnections(connectionStore)
	resolutionStore := resolutionpg.NewStore(db)
	extractionStore := extractionpg.NewStore(db)
	extractionReads := extractionquery.NewExtractions(extractionStore, bytes)
	relations := reviewquery.NewRelations(reviewStore)
	board := reviewquery.NewBoard(reviewStore)
	sourceLinks := reviewquery.NewSourceLinks(reviewStore)
	clusters := reviewquery.NewClusters(reviewStore)
	clusterCoverage := reviewquery.NewClusterCoverage(reviewStore)
	providerPolicy := assistquery.NewProviderPolicies(assistanceStore)
	return &research{
		sources:               reads,
		sourceCmd:             sourcecmd.NewSources(sourceStore, bytes, db, publisher, ids, clk),
		observations:          obsquery.NewManualObservations(observations),
		observationCmd:        obscmd.NewManualObservations(observations, retainedSources{sources: reads, extractions: extractionReads}, db, publisher, ids, clk),
		relations:             relations,
		board:                 board,
		sourceLinks:           sourceLinks,
		relationCmd:           reviewcmd.NewRelations(reviewStore, db, publisher, ids, clk),
		sourceLinkCmd:         reviewcmd.NewSourceLinks(reviewStore, db, publisher, ids, clk),
		clusters:              clusters,
		clusterCoverage:       clusterCoverage,
		clusterCmd:            reviewcmd.NewClusters(reviewStore, db, publisher, ids, clk),
		questions:             leadquery.NewQuestions(leadStore),
		questionCmd:           leadcmd.NewQuestions(leadStore, db, publisher, ids, clk),
		assistance:            assistquery.NewOperations(assistanceStore),
		providerRuns:          assistquery.NewProviderRuns(assistanceStore),
		providerPolicy:        providerPolicy,
		providerPolicyCmd:     assistcmd.NewProviderPolicies(assistanceStore, db, publisher, ids, clk),
		assistanceCmd:         assistcmd.NewOperations(assistanceStore, assistanceCaptures{sources: reads, extractions: extractionReads}, provider, providerPolicy, db, publisher, ids, clk),
		comparisons:           assistquery.NewComparisons(assistanceStore),
		comparisonCmd:         assistcmd.NewComparisonsWithPolicy(assistanceStore, comparisonEvidence{relations: relations}, comparisonProvider, providerPolicy, db, publisher, ids, clk),
		questionSuggestions:   assistquery.NewQuestionSuggestions(assistanceStore),
		questionSuggestionCmd: assistcmd.NewQuestionSuggestionsWithPolicy(assistanceStore, questionSuggestionEvidence{relations: relations}, questionSuggestionProvider, providerPolicy, db, publisher, ids, clk),
		briefDrafts:           assistquery.NewBriefDrafts(assistanceStore),
		briefDraftCmd:         assistcmd.NewBriefDraftsWithPolicy(assistanceStore, briefDraftEvidence{brief: briefReads, relations: relations}, briefDraftProvider, providerPolicy, db, publisher, ids, clk),
		connectionReviews:     assistquery.NewConnectionReviews(assistanceStore),
		connectionReviewCmd:   assistcmd.NewConnectionReviewsWithPolicy(assistanceStore, connections, connectionReviewEvidence{relations: relations}, connectionReviewProvider, providerPolicy, db, publisher, ids, clk),
		syntheses:             assistquery.NewSyntheses(assistanceStore),
		synthesisCmd:          assistcmd.NewSynthesesWithPolicy(assistanceStore, synthesisEvidence{relations: relations}, synthesisProvider, providerPolicy, db, publisher, ids, clk),
		events:                eventquery.NewEvents(eventStore),
		eventCmd:              eventcmd.NewEvents(eventStore, recordStore, db, publisher, ids, clk),
		eventAccounts:         eventquery.NewAccounts(eventStore),
		eventAccountCmd:       eventcmd.NewAccounts(eventStore, eventquery.NewEvents(eventStore), recordStore, db, publisher, ids, clk),
		eventClusters:         eventquery.NewClusters(eventStore),
		eventClusterCmd:       eventcmd.NewClusters(eventStore, eventquery.NewEvents(eventStore), db, publisher, ids, clk),
		eventRelationships:    eventquery.NewRelationships(eventStore),
		eventRelationshipCmd:  eventcmd.NewRelationships(eventStore, eventquery.NewEvents(eventStore), relations, db, publisher, ids, clk),
		brief:                 briefReads,
		briefCmd:              briefcmd.NewBriefs(briefStore, db, publisher, ids, clk),
		records:               recordquery.NewRecords(recordStore),
		recordCmd:             recordcmd.NewRecords(recordStore, db, publisher, ids, clk),
		resolutions:           resolutionquery.NewResolutions(resolutionStore, resolutionStore),
		resolutionCmd:         resolutioncmd.NewResolutions(resolutionStore, recordStore, db, publisher, ids, clk, resolutionStore),
		connections:           connections,
		connectionCmd:         connectioncmd.NewConnections(connectionStore, db, publisher, ids, clk),
		extractions:           extractionReads,
		extractionCmd:         extractioncmd.NewExtractionsWithOCR(extractionStore, bytes, extractionCaptures{reads}, db, publisher, ids, clk, ocr),
		cleanup:               cleanupapp.New(cleanuppg.NewStore(db), bytes, publisher, ids, clk),
		fetcher:               fetcher,
	}
}

type synthesisEvidence struct{ relations *reviewquery.Relations }

type comparisonEvidence struct{ relations *reviewquery.Relations }

type questionSuggestionEvidence struct{ relations *reviewquery.Relations }

type briefDraftEvidence struct {
	brief     *briefquery.Briefs
	relations *reviewquery.Relations
}

type connectionReviewEvidence struct{ relations *reviewquery.Relations }

func (s synthesisEvidence) Evidence(ctx context.Context, workspace, observation id.ID) (assistapp.Observation, error) {
	found, err := s.relations.EvidenceByID(ctx, workspace, observation)
	if err != nil {
		return assistapp.Observation{}, err
	}
	return assistapp.Observation{ID: found.ID, WorkspaceID: found.WorkspaceID, SourceTitle: found.SourceTitle, Statement: found.Statement, Quote: found.Quote}, nil
}

func (s comparisonEvidence) LoadComparisonInput(ctx context.Context, workspace id.ID, observations []id.ID) (assistapp.ComparisonInput, error) {
	inputs := make([]assistapp.Observation, 0, len(observations))
	selected := make(map[id.ID]struct{}, len(observations))
	for _, observation := range observations {
		found, err := s.relations.EvidenceByID(ctx, workspace, observation)
		if err != nil {
			return assistapp.ComparisonInput{}, err
		}
		selected[observation] = struct{}{}
		inputs = append(inputs, assistapp.Observation{ID: found.ID, WorkspaceID: found.WorkspaceID, SourceTitle: found.SourceTitle, Statement: found.Statement, Quote: found.Quote})
	}
	decisions := make([]assistapp.ComparisonDecision, 0)
	var before id.ID
	for {
		page, err := s.relations.Relations(ctx, workspace, before, 100)
		if err != nil {
			return assistapp.ComparisonInput{}, err
		}
		for _, relation := range page.Items {
			if _, left := selected[relation.LeftObservationID]; !left {
				continue
			}
			if _, right := selected[relation.RightObservationID]; !right {
				continue
			}
			decisions = append(decisions, assistapp.ComparisonDecision{LeftObservationID: relation.LeftObservationID, RightObservationID: relation.RightObservationID, Kind: relation.Kind.String(), Rationale: relation.Rationale})
		}
		if page.NextCursor == nil {
			break
		}
		before = *page.NextCursor
	}
	return assistapp.ComparisonInput{Observations: inputs, Decisions: decisions}, nil
}

func (s questionSuggestionEvidence) LoadQuestionSuggestionInput(ctx context.Context, workspace id.ID, gaps []assistdomain.QuestionSuggestionGap) (assistapp.QuestionSuggestionInput, error) {
	loaded := make(map[id.ID]assistapp.Observation)
	for _, gap := range gaps {
		for _, observation := range gap.ObservationIDs {
			if _, ok := loaded[observation]; ok {
				continue
			}
			found, err := s.relations.EvidenceByID(ctx, workspace, observation)
			if err != nil {
				return assistapp.QuestionSuggestionInput{}, err
			}
			loaded[observation] = assistapp.Observation{ID: found.ID, WorkspaceID: found.WorkspaceID, SourceTitle: found.SourceTitle, Statement: found.Statement, Quote: found.Quote}
		}
	}
	input := assistapp.QuestionSuggestionInput{Gaps: make([]assistapp.QuestionSuggestionGapInput, 0, len(gaps))}
	for _, gap := range gaps {
		observations := make([]assistapp.Observation, 0, len(gap.ObservationIDs))
		for _, observation := range gap.ObservationIDs {
			observations = append(observations, loaded[observation])
		}
		input.Gaps = append(input.Gaps, assistapp.QuestionSuggestionGapInput{Kind: gap.Kind, Label: gap.Label, Detail: gap.Detail, ObservationIDs: append([]id.ID(nil), gap.ObservationIDs...), Observations: observations})
	}
	return input, nil
}

func (s briefDraftEvidence) LoadBriefDraftInput(ctx context.Context, workspace id.ID, observations []id.ID) (assistapp.BriefDraftInput, error) {
	brief, err := s.brief.ByWorkspace(ctx, workspace)
	if err != nil {
		return assistapp.BriefDraftInput{}, err
	}
	input := assistapp.BriefDraftInput{Brief: assistdomain.BriefDraftInput{BriefID: brief.ID, Title: brief.Title, Question: brief.Question, CurrentAccount: brief.CurrentAccount, Alternatives: brief.Alternatives, Limitations: brief.Limitations, NextSteps: brief.NextSteps, ObservationIDs: append([]id.ID(nil), observations...)}, Observations: make([]assistapp.BriefDraftObservation, 0, len(observations))}
	for _, observation := range observations {
		found, err := s.relations.EvidenceByID(ctx, workspace, observation)
		if err != nil {
			return assistapp.BriefDraftInput{}, err
		}
		input.Observations = append(input.Observations, assistapp.BriefDraftObservation{ID: found.ID, SourceTitle: found.SourceTitle, Statement: found.Statement, Quote: found.Quote})
	}
	return input, nil
}

func (s connectionReviewEvidence) LoadConnectionReviewInput(ctx context.Context, connection connectiondomain.Connection) (assistapp.ConnectionReviewInput, error) {
	load := func(observations []id.ID) ([]assistapp.Observation, error) {
		out := make([]assistapp.Observation, 0, len(observations))
		for _, observation := range observations {
			found, err := s.relations.EvidenceByID(ctx, connection.WorkspaceID, observation)
			if err != nil {
				return nil, err
			}
			out = append(out, assistapp.Observation{ID: found.ID, WorkspaceID: found.WorkspaceID, SourceTitle: found.SourceTitle, Statement: found.Statement, Quote: found.Quote})
		}
		return out, nil
	}
	supporting, err := load(connection.SupportingObservationIDs)
	if err != nil {
		return assistapp.ConnectionReviewInput{}, err
	}
	opposing, err := load(connection.OpposingObservationIDs)
	if err != nil {
		return assistapp.ConnectionReviewInput{}, err
	}
	selected := make(map[id.ID]struct{}, len(connection.SupportingObservationIDs)+len(connection.OpposingObservationIDs))
	for _, observation := range append(append([]id.ID{}, connection.SupportingObservationIDs...), connection.OpposingObservationIDs...) {
		selected[observation] = struct{}{}
	}
	decisions := make([]assistapp.ComparisonDecision, 0)
	var before id.ID
	for {
		page, err := s.relations.Relations(ctx, connection.WorkspaceID, before, 100)
		if err != nil {
			return assistapp.ConnectionReviewInput{}, err
		}
		for _, relation := range page.Items {
			if _, ok := selected[relation.LeftObservationID]; !ok {
				continue
			}
			if _, ok := selected[relation.RightObservationID]; !ok {
				continue
			}
			decisions = append(decisions, assistapp.ComparisonDecision{LeftObservationID: relation.LeftObservationID, RightObservationID: relation.RightObservationID, Kind: relation.Kind.String(), Rationale: relation.Rationale})
		}
		if page.NextCursor == nil {
			break
		}
		before = *page.NextCursor
	}
	return assistapp.ConnectionReviewInput{
		ConnectionID: connection.ID, FromRecordID: connection.FromRecordID, ToRecordID: connection.ToRecordID,
		ConnectionKind: connection.Kind.String(), ConnectionState: connection.State.String(), ConnectionRationale: connection.Rationale,
		SupportingObservationIDs: append([]id.ID(nil), connection.SupportingObservationIDs...), OpposingObservationIDs: append([]id.ID(nil), connection.OpposingObservationIDs...),
		SupportingObservations: supporting, OpposingObservations: opposing, Decisions: decisions,
	}, nil
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
		return assistcmd.RetainedCapture{}, fmt.Errorf("%w: %v", assistdomain.ErrUnsupported, extractiondomain.ErrUnsupported)
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
	mux.HandleFunc("GET /v1/workspaces/{workspace}/source-intake", m.listSourceIntake)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/source-intake", m.createSourceIntake)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/source-intake/{intake}", m.readSourceIntake)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/source-intake/{intake}/review", m.reviewSourceIntake)
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
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/sources/{source}/publication", m.setSourcePublication)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/sources/{source}/duplicate-policy", m.setSourceDuplicatePolicy)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/sources/{source}/privacy", m.setSourcePrivacy)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/retention-review", m.reviewSourceRetention)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/purge", m.purgeSource)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/captures/{capture}", m.readSourceCapture)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/captures/{capture}/extractions", m.listSourceExtractions)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/captures/{capture}/extract", m.extractSourceCapture)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/captures/{capture}/extractions/{extraction}", m.readSourceExtraction)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/captures", m.captureSource)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/fetch", m.fetchSource)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/watch", m.readSourceWatch)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/sources/{source}/watch", m.setSourceWatch)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/watch/run", m.runSourceWatch)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/source-watches/run-due", m.runDueSourceWatch)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/source-alerts", m.listSourceAlerts)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/source-alerts/refresh-gaps", m.refreshSourceGapAlerts)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/source-alerts/{alert}/seen", m.markSourceAlertSeen)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/source-alert-delivery", m.readSourceAlertDelivery)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/source-alert-delivery", m.saveSourceAlertDelivery)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/observations", m.listSourceObservations)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/observations", m.recordSourceObservation)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/observations/{observation}/shares", m.listCitationShares)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/observations/{observation}/shares", m.createCitationShare)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/shares/{share}/revoke", m.revokeCitationShare)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/sources/{source}/observations/{observation}", m.readSourceObservation)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/questions", m.listQuestions)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/questions", m.createQuestion)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/questions/{question}", m.readQuestion)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/questions/{question}", m.editQuestion)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/events", m.listTimelineEvents)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/events", m.createTimelineEvent)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/events/{event}/revisions", m.listTimelineEventRevisions)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/events/{event}/revisions/{revision}", m.readTimelineEventRevision)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/events/{event}", m.readTimelineEvent)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/events/{event}", m.editTimelineEvent)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/records/{record}/neighborhood", m.readResearchRecordNeighborhood)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/events/{event}/accounts", m.listTimelineEventAccounts)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/events/{event}/accounts", m.createTimelineEventAccount)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/events/{event}/accounts/reconciliation", m.reconcileTimelineEventAccounts)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/event-clusters", m.listTimelineEventClusters)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/event-clusters", m.createTimelineEventCluster)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/event-clusters/{cluster}", m.readTimelineEventCluster)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/event-clusters/{cluster}", m.editTimelineEventCluster)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/event-clusters/{cluster}/review", m.reviewTimelineEventCluster)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/event-relationships", m.listTimelineEventRelationships)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/event-relationships", m.createTimelineEventRelationship)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/event-relationships/{relationship}", m.readTimelineEventRelationship)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/event-relationships/{relationship}/review", m.reviewTimelineEventRelationship)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief", m.readWorkingBrief)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/brief", m.saveWorkingBrief)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/drafts", m.listBriefDrafts)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/drafts/{draft}", m.readBriefDraft)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/brief/drafts", m.createBriefDraft)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/handoffs", m.listBriefRecipientHandoffs)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/handoffs/{snapshot}", m.readBriefRecipientHandoff)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/handoffs/{snapshot}/export", m.readBriefRecipientHandoffExport)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/shared/{token}", m.readBriefSharedHandoff)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/shared/{token}/export", m.readBriefSharedHandoffExport)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/snapshots", m.listBriefSnapshots)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/brief/snapshots", m.createBriefSnapshot)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/snapshots/{snapshot}", m.readBriefSnapshot)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/snapshots/{snapshot}/activity", m.briefSnapshotActivity)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/snapshots/{snapshot}/shares", m.listBriefSnapshotShares)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/brief/snapshots/{snapshot}/shares", m.createBriefSnapshotShare)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/brief/shares/{share}/revoke", m.revokeBriefSnapshotShare)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/snapshots/{snapshot}/comments", m.listBriefSnapshotComments)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/brief/snapshots/{snapshot}/comments", m.addBriefSnapshotComment)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/brief/snapshots/{snapshot}/review", m.readBriefSnapshotReview)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/brief/snapshots/{snapshot}/review/assignment", m.assignBriefSnapshotReviewer)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/brief/snapshots/{snapshot}/review/decisions", m.decideBriefSnapshotReview)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/records", m.listResearchRecords)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/records/summary", m.summarizeResearchRecords)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/records", m.createResearchRecord)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/records/{record}", m.readResearchRecord)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/records/{record}", m.editResearchRecord)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/records/{record}/resolutions", m.listRecordResolutions)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/records/{record}/resolutions", m.createRecordResolution)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/resolutions", m.listResearchResolutions)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/resolutions/{resolution}", m.readResearchResolution)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/resolutions/{resolution}/impact", m.readResearchResolutionImpact)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/resolutions/{resolution}", m.reviewResearchResolution)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/resolutions/{resolution}/reverse", m.reverseResearchResolution)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/resolution-sets", m.listResearchResolutionSets)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/resolution-sets", m.createResearchResolutionSet)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/resolution-sets/{resolution_set}", m.readResearchResolutionSet)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/resolution-sets/{resolution_set}/impact", m.readResearchResolutionSetImpact)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/resolution-sets/{resolution_set}", m.reviewResearchResolutionSet)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/resolution-sets/{resolution_set}/reverse", m.reverseResearchResolutionSet)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections", m.listResearchConnections)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections/summary", m.summarizeResearchConnections)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/connections", m.createResearchConnection)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections/{connection}/revisions", m.listResearchConnectionRevisions)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections/{connection}/revisions/{revision}", m.readResearchConnectionRevision)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections/{connection}/reviews", m.listResearchConnectionReviews)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/connections/{connection}/reviews", m.createResearchConnectionReview)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections/{connection}/reviews/{review}", m.readResearchConnectionReview)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/connections/{connection}", m.readResearchConnection)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/connections/{connection}", m.editResearchConnection)
	mux.HandleFunc("POST /v1/workspaces/{workspace}/sources/{source}/captures/{capture}/assistance", m.generateAssistance)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/assistance/policy", m.readAssistancePolicy)
	mux.HandleFunc("PUT /v1/workspaces/{workspace}/assistance/policy", m.updateAssistancePolicy)
	mux.HandleFunc("GET /v1/workspaces/{workspace}/assistance/runs", m.listAssistanceProviderRuns)
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
	caller, workspace, org, ok := m.onWorkspaceRecord(w, r)
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
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.sources.List(r.Context(), workspace, before, query, size, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) listSourceIntake(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	found, err := m.research.sources.ListIntake(r.Context(), workspace, before, status, size)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

type sourceIntakeRequest struct {
	Title         string `json:"title"`
	Origin        string `json:"origin"`
	URL           string `json:"url"`
	Filename      string `json:"filename"`
	MediaType     string `json:"media_type"`
	Content       string `json:"content"`
	ContentBase64 string `json:"content_base64"`
	Note          string `json:"note"`
}

func (m *me) createSourceIntake(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	var in sourceIntakeRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	var fresh sourcedomain.IntakeCandidate
	var err error
	switch strings.TrimSpace(in.Origin) {
	case "", sourcedomain.IntakeReference:
		fresh, err = m.research.sourceCmd.CreateIntakeCandidate(r.Context(), workspace, caller, in.Title, in.URL, in.Note)
	case sourcedomain.IntakeImport:
		content := []byte(in.Content)
		if strings.TrimSpace(in.ContentBase64) != "" {
			content, err = base64.StdEncoding.DecodeString(strings.TrimSpace(in.ContentBase64))
			if err != nil {
				httpx.WriteError(w, r, errors.New(errors.Invalid, "import content must be valid base64"))
				return
			}
		}
		fresh, err = m.research.sourceCmd.CreateImportIntakeCandidate(r.Context(), workspace, caller, in.Title, in.Filename, in.MediaType, content, in.Note)
	default:
		httpx.WriteError(w, r, errors.New(errors.Invalid, "source intake origin must be reference or import"))
		return
	}
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, fresh)
}

func (m *me) readSourceIntake(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	intake, ok := m.sourceID(w, r, "intake")
	if !ok {
		return
	}
	found, err := m.research.sources.ReadIntake(r.Context(), workspace, intake)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

type sourceIntakeReviewRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (m *me) reviewSourceIntake(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	intake, ok := m.sourceID(w, r, "intake")
	if !ok {
		return
	}
	var in sourceIntakeReviewRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	found, err := m.research.sourceCmd.ReviewIntakeCandidate(r.Context(), workspace, intake, caller, strings.TrimSpace(in.Decision), in.Note)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}

func (m *me) searchResearch(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspaceRecord(w, r)
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
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	found, err := m.research.sources.Search(r.Context(), workspace, before, query, size, maxSensitivity)
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

type sourcePublicationRequest struct {
	PublishedAt *string `json:"published_at"`
}

func (m *me) setSourcePublication(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	var in sourcePublicationRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	var publishedAt *time.Time
	if in.PublishedAt != nil {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*in.PublishedAt))
		if err != nil {
			httpx.Fail(m.log, w, r, sourcedomain.ErrPublicationInvalid)
			return
		}
		publishedAt = &parsed
	}
	if err := m.research.sourceCmd.SetPublication(r.Context(), workspace, source, caller, publishedAt); err != nil {
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

type sourceDuplicatePolicyRequest struct {
	DuplicatePolicy string `json:"duplicate_policy"`
}

func (m *me) setSourceDuplicatePolicy(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	var in sourceDuplicatePolicyRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	if err := m.research.sourceCmd.SetDuplicatePolicy(r.Context(), workspace, source, caller, strings.TrimSpace(in.DuplicatePolicy)); err != nil {
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
		httpx.WriteError(w, r, egress.ResponseError(response))
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

type sourceWatchRequest struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"interval_seconds"`
}

func (m *me) readSourceWatch(w http.ResponseWriter, r *http.Request) {
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
	watch, err := m.research.sources.Watch(r.Context(), workspace, source)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, watch)
}

func (m *me) setSourceWatch(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	var in sourceWatchRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	watch, err := m.research.sourceCmd.ConfigureWatch(r.Context(), workspace, source, caller, in.Enabled, in.IntervalSeconds)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, watch)
}

func (m *me) listSourceAlerts(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	before, size, ok := researchPage(w, r)
	if !ok {
		return
	}
	maxSensitivity, err := m.sourceSensitivityScope(r.Context(), caller, workspace, org)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	alerts, err := m.research.sources.Alerts(r.Context(), workspace, caller, before, size, maxSensitivity)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, alerts)
}

type sourceAlertSeenResponse struct {
	SeenAt string `json:"seen_at"`
}

func (m *me) markSourceAlertSeen(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	alert, ok := m.sourceID(w, r, "alert")
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		return
	}
	at, err := m.research.sourceCmd.MarkAlertSeen(r.Context(), workspace, alert, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, sourceAlertSeenResponse{SeenAt: at.UTC().Format(time.RFC3339Nano)})
}

func (m *me) readSourceAlertDelivery(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspaceRecord(w, r)
	if !ok {
		return
	}
	preference, err := m.research.sourceCmd.AlertDelivery(r.Context(), workspace, caller)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, preference)
}

type sourceAlertDeliveryRequest struct {
	EmailEnabled bool     `json:"email_enabled"`
	Kinds        []string `json:"kinds"`
}

func (m *me) saveSourceAlertDelivery(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelRead)
	if !ok {
		return
	}
	var in sourceAlertDeliveryRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	preference, err := m.research.sourceCmd.SaveAlertDelivery(r.Context(), workspace, caller, in.EmailEnabled, in.Kinds)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, preference)
}

func (m *me) runSourceWatch(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	source, ok := m.sourceID(w, r, "source")
	if !ok {
		return
	}
	result, err := m.executeSourceWatch(r.Context(), workspace, source, caller, "")
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, result)
}

type dueSourceWatchResponse struct {
	Claimed bool                      `json:"claimed"`
	Run     *sourcecmd.WatchRunResult `json:"run,omitempty"`
}

func (m *me) runDueSourceWatch(w http.ResponseWriter, r *http.Request) {
	caller, workspace, _, ok := m.onWorkspace(w, r, orgdomain.LevelWrite)
	if !ok {
		return
	}
	owner := "watch-worker/" + caller.String() + "/" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	claimed, err := m.research.sourceCmd.ClaimDueWatch(r.Context(), workspace, owner, 2*time.Minute)
	if stderrors.Is(err, sourcedomain.ErrWatchNotDue) {
		httpx.WriteJSON(w, r, http.StatusOK, dueSourceWatchResponse{Claimed: false})
		return
	}
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	run, err := m.executeSourceWatch(r.Context(), workspace, claimed.SourceID, caller, owner)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, dueSourceWatchResponse{Claimed: true, Run: &run})
}

func (m *me) executeSourceWatch(ctx context.Context, workspace, source, caller id.ID, leaseOwner string) (sourcecmd.WatchRunResult, error) {
	detail, err := m.research.sources.Read(ctx, workspace, source)
	if err != nil {
		return sourcecmd.WatchRunResult{}, err
	}
	watch, err := m.research.sources.Watch(ctx, workspace, source)
	if err != nil {
		return sourcecmd.WatchRunResult{}, err
	}
	if !watch.Enabled {
		return sourcecmd.WatchRunResult{}, errors.New(errors.Invalid, "enable source monitoring before running a watch")
	}
	if watch.LeaseOwner != "" && watch.LeaseOwner != leaseOwner {
		return sourcecmd.WatchRunResult{}, sourcedomain.ErrWatchLeased
	}
	fail := func(runErr error) {
		var persistErr error
		if leaseOwner == "" {
			_, persistErr = m.research.sourceCmd.RecordWatchRun(ctx, workspace, source, caller, sourcedomain.WatchStatusFailed, id.ID{}, boundedWatchError(runErr))
		} else {
			_, persistErr = m.research.sourceCmd.RecordClaimedWatchRun(ctx, workspace, source, caller, sourcedomain.WatchStatusFailed, id.ID{}, boundedWatchError(runErr), leaseOwner)
		}
		if persistErr != nil {
			runErr = persistErr
		}
		err = runErr
	}
	response, err := m.research.fetcher.Get(ctx, detail.Source.URL)
	if err != nil {
		fail(err)
		return sourcecmd.WatchRunResult{}, err
	}
	if response.Status < http.StatusOK || response.Status >= http.StatusMultipleChoices {
		err = egress.ResponseError(response)
		fail(err)
		return sourcecmd.WatchRunResult{}, err
	}
	mediaType, err := fetchedMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		fail(err)
		return sourcecmd.WatchRunResult{}, err
	}
	if response.Truncated {
		err = egress.ErrTooLarge
		fail(err)
		return sourcecmd.WatchRunResult{}, err
	}
	digest := sha256.Sum256(response.Body)
	sha := hex.EncodeToString(digest[:])
	// Re-read after the network call. A lease can expire while a slow reference
	// responds; another worker may have completed the same capture in the
	// meantime, and the latest retained hash is the authoritative change check.
	latest, latestErr := m.research.sources.Read(ctx, workspace, source)
	if latestErr != nil {
		fail(latestErr)
		return sourcecmd.WatchRunResult{}, latestErr
	}
	if latest.Source.LatestCapture != nil && latest.Source.LatestCapture.SHA256 == sha {
		var updated sourcedomain.Watch
		if leaseOwner == "" {
			updated, err = m.research.sourceCmd.RecordWatchRun(ctx, workspace, source, caller, sourcedomain.WatchStatusUnchanged, id.ID{}, "")
		} else {
			updated, err = m.research.sourceCmd.RecordClaimedWatchRun(ctx, workspace, source, caller, sourcedomain.WatchStatusUnchanged, id.ID{}, "", leaseOwner)
		}
		if err != nil {
			return sourcecmd.WatchRunResult{}, err
		}
		return sourcecmd.WatchRunResult{Watch: updated, Changed: false}, nil
	}
	var captured sourcedomain.Capture
	if mediaType == "application/pdf" || mediaType == "image/png" || mediaType == "image/jpeg" || mediaType == "image/webp" {
		captured, err = m.research.sourceCmd.AddBinaryCapture(ctx, workspace, source, caller, mediaType, response.Body)
	} else {
		captured, err = m.research.sourceCmd.AddCapture(ctx, workspace, source, caller, mediaType, string(response.Body))
	}
	if err != nil {
		fail(err)
		return sourcecmd.WatchRunResult{}, err
	}
	var updated sourcedomain.Watch
	if leaseOwner == "" {
		updated, err = m.research.sourceCmd.RecordWatchRun(ctx, workspace, source, caller, sourcedomain.WatchStatusChanged, captured.ID, "")
	} else {
		updated, err = m.research.sourceCmd.RecordClaimedWatchRun(ctx, workspace, source, caller, sourcedomain.WatchStatusChanged, captured.ID, "", leaseOwner)
	}
	if err != nil {
		return sourcecmd.WatchRunResult{}, err
	}
	return sourcecmd.WatchRunResult{Watch: updated, Changed: true, Capture: &captured}, nil
}

func boundedWatchError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 2000 {
		return message[:2000]
	}
	return message
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
		ExtractionID     id.ID `json:"extraction_id,omitempty"`
		RetryOperationID id.ID `json:"retry_operation_id,omitempty"`
	}
	if !decodeResearchBody(w, r, &in) {
		return
	}
	var operation assistdomain.Operation
	var proposals []assistdomain.Proposal
	var err error
	if !in.RetryOperationID.IsZero() {
		operation, proposals, err = m.research.assistanceCmd.Retry(r.Context(), workspace, source, capture, in.RetryOperationID, caller)
	} else {
		operation, proposals, err = m.research.assistanceCmd.Generate(r.Context(), workspace, source, capture, in.ExtractionID, caller)
	}
	if err != nil {
		if !operation.ID.IsZero() {
			httpx.WriteJSON(w, r, http.StatusCreated, assistquery.Detail{Operation: operation, Proposals: proposals})
			return
		}
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, assistquery.Detail{Operation: operation, Proposals: proposals})
}

type assistancePolicyRequest struct {
	AllowExternal bool `json:"allow_external"`
}

func (m *me) readAssistancePolicy(w http.ResponseWriter, r *http.Request) {
	_, workspace, _, ok := m.onWorkspaceOrgMember(w, r)
	if !ok {
		return
	}
	policy, err := m.research.providerPolicy.Current(r.Context(), workspace)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, policy)
}

func (m *me) updateAssistancePolicy(w http.ResponseWriter, r *http.Request) {
	caller, workspace, org, ok := m.onWorkspaceOrgMember(w, r)
	if !ok {
		return
	}
	reach, err := m.access.In(r.Context(), caller, org)
	if err != nil {
		httpx.Fail(m.log, w, r, orgquery.ErrNoAccess)
		return
	}
	if reach.Role != orgdomain.RoleOwner && reach.Role != orgdomain.RoleAdmin {
		httpx.WriteError(w, r, errors.New(errors.Forbidden, "only an organisation admin can change the assistance provider policy"))
		return
	}
	var in assistancePolicyRequest
	if !decodeResearchBody(w, r, &in) {
		return
	}
	policy, err := m.research.providerPolicyCmd.Set(r.Context(), workspace, caller, in.AllowExternal)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, policy)
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
