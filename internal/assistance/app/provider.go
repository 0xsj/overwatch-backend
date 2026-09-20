package app

import (
	"context"
	"strings"
	"unicode"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Input is intentionally one retained capture. The provider cannot ask for a
// workspace-wide corpus or silently add a second source to an operation.
type Input struct {
	SourceID     id.ID
	CaptureID    id.ID
	ExtractionID id.ID
	Content      string
}

type Provider interface {
	Extract(context.Context, Input) ([]domain.ProposalDraft, error)
	Name() string
	Method() string
}

// ExternalProvider is implemented by providers that send retained material
// outside the Overwatch process. Unknown providers fail closed and are treated
// as external by the policy gate.
type ExternalProvider interface {
	External() bool
}

// LocalSentenceProvider is a deterministic first provider. It identifies
// sentence-like passages and returns them unchanged, which makes the feature
// useful without an external model while keeping its output visibly a draft.
type LocalSentenceProvider struct{}

func (LocalSentenceProvider) Name() string            { return "local" }
func (LocalSentenceProvider) Method() string          { return "sentence-passages-v1" }
func (LocalSentenceProvider) TemplateVersion() string { return "sentence-passages-v1" }
func (LocalSentenceProvider) External() bool          { return false }

func (LocalSentenceProvider) Extract(ctx context.Context, in Input) ([]domain.ProposalDraft, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runes := []rune(in.Content)
	out := make([]domain.ProposalDraft, 0, domain.MaxProposals)
	seen := make(map[string]struct{})
	start := 0
	flush := func(end int) {
		for start < end && unicode.IsSpace(runes[start]) {
			start++
		}
		for end > start && unicode.IsSpace(runes[end-1]) {
			end--
		}
		if end-start < 12 {
			return
		}
		quote := string(runes[start:end])
		if _, exists := seen[quote]; exists {
			return
		}
		seen[quote] = struct{}{}
		out = append(out, domain.ProposalDraft{Statement: quote, Quote: quote, QuoteStart: start, QuoteEnd: end})
	}
	for i, r := range runes {
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			flush(i + 1)
			start = i + 1
			if len(out) == domain.MaxProposals {
				break
			}
		}
	}
	if len(out) < domain.MaxProposals && start < len(runes) {
		flush(len(runes))
	}
	if len(out) == 0 && strings.TrimSpace(in.Content) != "" {
		// A paragraph without punctuation is still a useful review target. The
		// domain will enforce the maximum citation size before persistence.
		end := len(runes)
		for end > 0 && unicode.IsSpace(runes[end-1]) {
			end--
		}
		if end > 0 {
			begin := 0
			for begin < end && unicode.IsSpace(runes[begin]) {
				begin++
			}
			quote := string(runes[begin:end])
			if len(quote) <= 8000 {
				out = append(out, domain.ProposalDraft{Statement: quote, Quote: quote, QuoteStart: begin, QuoteEnd: end})
			}
		}
	}
	return out, nil
}
