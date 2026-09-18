// Author-written, from decisions/0040's Verification block, which was written
// before this code. The resolve/unresolve split is the whole of §5 and it is
// what nothing else can check: the store answers "no such fragment" and the
// assembler decides what that means.
package command_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/entity/app/command"
	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

var (
	space      = an(0x10)
	invocation = an(0x11)
	target     = an(0x12)
	artifact   = an(0x13)
	parser     = an(0x14)
	permitted  = an(0x15)
)

// ---------------------------------------------------------------- the fakes

// store holds fragments by their folded tuple, which is what the real one keys
// on — a fake keyed differently would hide exactly the fold mismatch 0040 §5
// exists to make visible.
type store struct {
	fragments  map[string]domain.Fragment
	root       domain.Entity
	drawn      []domain.Derivation
	unresolved []domain.Unresolved
}

func newStore() *store {
	return &store{
		fragments: map[string]domain.Fragment{},
		root:      domain.Entity{ID: an(0x20), WorkspaceID: space, Kind: "org", Label: "acme"},
	}
}

func key(kind, value string) string { return kind + "\x00" + domain.Fold(value) }

func (s *store) hold(kind, value string, n byte) domain.Fragment {
	f := domain.Fragment{ID: an(n), WorkspaceID: space, Kind: kind, Value: domain.Fold(value)}
	s.fragments[key(kind, value)] = f
	return f
}

func (s *store) CreateEntity(context.Context, domain.Entity) error { return nil }
func (s *store) EntityByID(context.Context, id.ID, id.ID) (domain.Entity, error) {
	return s.root, nil
}
func (s *store) RootFor(context.Context, id.ID) (domain.Entity, error) { return s.root, nil }
func (s *store) JudgeEntity(context.Context, domain.Entity) error      { return nil }

func (s *store) Upsert(_ context.Context, f domain.Fragment) (domain.Fragment, bool, error) {
	k := key(f.Kind, f.Value)
	held, ok := s.fragments[k]
	if ok {
		return held, false, nil
	}
	s.fragments[k] = f
	return f, true, nil
}

func (s *store) FragmentByID(context.Context, id.ID, id.ID) (domain.Fragment, error) {
	return domain.Fragment{}, nil
}
func (s *store) JudgeFragment(context.Context, domain.Fragment) error { return nil }
func (s *store) MarkRead(context.Context, domain.Fragment) error      { return nil }

func (s *store) FragmentFor(_ context.Context, _ id.ID, kind, value string) (domain.Fragment, bool, error) {
	f, ok := s.fragments[key(kind, value)]
	return f, ok, nil
}

func (s *store) Draw(_ context.Context, d domain.Derivation) error {
	s.drawn = append(s.drawn, d)
	return nil
}

func (s *store) RecordUnresolved(_ context.Context, u domain.Unresolved) error {
	s.unresolved = append(s.unresolved, u)
	return nil
}

func (s *store) Attribute(context.Context, domain.Attribution) error { return nil }
func (s *store) AttributionByID(context.Context, id.ID, id.ID) (domain.Attribution, error) {
	return domain.Attribution{}, nil
}
func (s *store) Decide(context.Context, domain.Attribution) error { return nil }

type saw struct{ subjects []command.Subject }

func (s saw) ForInvocation(context.Context, id.ID, id.ID) ([]command.Subject, error) {
	return s.subjects, nil
}

type cited struct{ rows []command.Provenance }

func (c cited) ForInvocation(context.Context, id.ID, id.ID) ([]command.Provenance, error) {
	return c.rows, nil
}

type aimedAt struct{}

func (aimedAt) TargetOf(context.Context, id.ID, id.ID) (id.ID, id.ID, error) {
	return target, permitted, nil
}

// covers says yes to everything, so an attribution never interferes with what
// these tests are about.
type covers struct{}

func (covers) MayClaim(context.Context, id.ID, id.ID, string, string, id.ID) (domain.Claim, error) {
	return domain.Claim{Covered: true, Rule: permitted, Basis: "in scope"}, nil
}

type minter struct{ n byte }

func (m *minter) NewID() id.ID {
	m.n++
	return an(0x80 + m.n)
}

type frozen struct{}

func (frozen) Now() time.Time { return at }

type quiet struct{}

func (quiet) Publish(context.Context, ...events.Event) error { return nil }

// ---------------------------------------------------------------- the rig

// httpx aimed at two hosts, emitting two urls, each echoing its input.
func rig(t *testing.T, repo *store, rows []command.Provenance) *command.Assembler {
	t.Helper()
	subjects := []command.Subject{
		{Kind: "url", Value: "https://a.acme.test/", Count: 1, LastSeen: at},
		{Kind: "url", Value: "https://b.acme.test/", Count: 1, LastSeen: at},
	}
	return command.NewAssembler(repo, saw{subjects: subjects}, cited{rows: rows},
		aimedAt{}, covers{}, quiet{}, &minter{}, frozen{})
}

func cite(subject, from string) command.Provenance {
	return command.Provenance{
		SubjectKind: "url", SubjectValue: subject,
		FromKind: "host", FromValue: from,
		Label: "input", MappingID: parser, ArtifactID: artifact,
	}
}

// ---------------------------------------------------------------- the claims

// 0040, and 0003's second edge kind finally existing: a record whose provenance
// resolves draws exactly one edge, carrying the invocation and the artifact.
func TestAResolvedProvenanceDrawsOneSourcedEdge(t *testing.T) {
	repo := newStore()
	fromA := repo.hold("host", "a.acme.test", 0x30)
	repo.hold("host", "b.acme.test", 0x31)

	got, err := rig(t, repo, []command.Provenance{
		cite("https://a.acme.test/", "a.acme.test"),
		cite("https://b.acme.test/", "b.acme.test"),
	}).Observed(context.Background(), space, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if got.Derivations != 2 || got.Unresolved != 0 {
		t.Fatalf("want 2 drawn and 0 unresolved, got %+v", got)
	}
	if len(repo.drawn) != 2 {
		t.Fatalf("edges written: %d", len(repo.drawn))
	}
	first := repo.drawn[0]
	if first.From != fromA.ID {
		t.Fatalf("from is the HOST it was read out of: %v", first.From)
	}
	if first.To == first.From {
		t.Fatal("to is the url that was read")
	}
	// 0003: never absent, either of them.
	if first.Invocation != invocation || first.Artifact != artifact {
		t.Fatalf("an edge must be sourceable: %+v", first)
	}
	if first.Label != "input" {
		t.Fatalf("the label names the act: %q", first.Label)
	}
	if first.Mapping != parser {
		t.Fatalf("an edge cites the version that read it: %v", first.Mapping)
	}
}

// 0040 §5. THE claim of that section: the tool named a host no fragment exists
// for — the ordinary reason being a candidate the spawn gate refused (0039) —
// and it is RECORDED rather than dropped or invented.
func TestAProvenanceWithNoFragmentIsRecordedAndDrawsNothing(t *testing.T) {
	repo := newStore()
	repo.hold("host", "a.acme.test", 0x30)
	// b.acme.test is deliberately ABSENT: the gate refused that candidate, so
	// no run ever observed it and no fragment was ever made.

	got, err := rig(t, repo, []command.Provenance{
		cite("https://a.acme.test/", "a.acme.test"),
		cite("https://b.acme.test/", "b.acme.test"),
	}).Observed(context.Background(), space, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if got.Derivations != 1 || got.Unresolved != 1 {
		t.Fatalf("want 1 drawn and 1 unresolved, got %+v", got)
	}
	if len(repo.unresolved) != 1 {
		t.Fatalf("unresolved rows: %d", len(repo.unresolved))
	}
	missed := repo.unresolved[0]
	if missed.FromValue != "b.acme.test" || missed.FromKind != "host" {
		t.Fatalf("the unresolved row names what was looked for: %+v", missed)
	}
	if missed.Invocation != invocation || missed.Label != "input" {
		t.Fatalf("and where it was cited: %+v", missed)
	}
	// AND THE FRAGMENT WAS NOT CREATED. Manufacturing it would resurrect
	// exactly the host a scope rule refused — 0040 §5's closer call.
	if _, ok, _ := repo.FragmentFor(context.Background(), space, "host", "b.acme.test"); ok {
		t.Fatal("an unresolved provenance must not create the fragment it names")
	}
}

// A FOLD MISMATCH is the other ordinary reason, and it must NOT be one: the
// lookup folds, so a tool echoing `ACME.test` finds the fragment stored as
// `acme.test`. 0037 named the fold mismatch as its own quiet failure.
func TestAToolEchoingADifferentCaseStillResolves(t *testing.T) {
	repo := newStore()
	repo.hold("host", "a.acme.test", 0x30)

	got, err := rig(t, repo, []command.Provenance{
		cite("https://a.acme.test/", "A.ACME.Test"),
	}).Observed(context.Background(), space, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if got.Derivations != 1 || got.Unresolved != 0 {
		t.Fatalf("a case difference is the same host: %+v", got)
	}
}

// Most tools declare no provenance mapping at all, and that must cost nothing
// and say nothing.
func TestNoProvenanceMeansNoEdgesAndNoRows(t *testing.T) {
	repo := newStore()
	got, err := rig(t, repo, nil).Observed(context.Background(), space, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if got.Derivations != 0 || got.Unresolved != 0 {
		t.Fatalf("nothing cited anything: %+v", got)
	}
	if len(repo.drawn) != 0 || len(repo.unresolved) != 0 {
		t.Fatal("no provenance mapping must write nothing")
	}
	// The FRAGMENTS still happened — this pass is additive to the one 0036
	// built, not a replacement for it.
	if got.Fragments != 2 {
		t.Fatalf("the fragment pass is unaffected: %+v", got)
	}
}

// A mapping pointed at its own subject would draw a self-edge on every record.
// It is refused in the domain, and here it must be SKIPPED rather than fatal:
// the rest of the delivery is still true.
func TestASelfEdgeIsSkippedAndDoesNotFailTheDelivery(t *testing.T) {
	repo := newStore()
	// The same fragment on both ends: the tool's `from` value resolves to the
	// url that was read.
	repo.fragments[key("url", "https://a.acme.test/")] = domain.Fragment{
		ID: an(0x30), WorkspaceID: space, Kind: "url", Value: "https://a.acme.test/",
	}
	repo.hold("host", "b.acme.test", 0x31)

	got, err := rig(t, repo, []command.Provenance{
		{SubjectKind: "url", SubjectValue: "https://a.acme.test/",
			FromKind: "url", FromValue: "https://a.acme.test/",
			Label: "input", MappingID: parser, ArtifactID: artifact},
		cite("https://b.acme.test/", "b.acme.test"),
	}).Observed(context.Background(), space, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if got.Derivations != 1 {
		t.Fatalf("the other edge still drew: %+v", got)
	}
	for _, d := range repo.drawn {
		if d.From == d.To {
			t.Fatal("a fragment is not read out of itself")
		}
	}
}
