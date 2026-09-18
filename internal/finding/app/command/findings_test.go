// Author-written, from decisions/0041. The command is where a sighting either
// becomes a finding or is SKIPPED, and where the two events are told apart —
// none of which the domain or the store can check on its own.
package command_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/finding/app/command"
	"github.com/0xsj/overwatch-backend/internal/finding/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 8, 2, 0, 0, 0, time.UTC)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

var (
	space    = an(0x10)
	tool     = an(0x11)
	fragment = an(0x12)
	person   = an(0x13)
)

// ---------------------------------------------------------------- the fakes

type store struct {
	held    map[string]domain.Finding
	details []domain.Detail
	saved   []domain.Finding
}

func newStore() *store { return &store{held: map[string]domain.Finding{}} }

func key(f domain.Finding) string { return f.ToolID.String() + "\x00" + f.Signature }

func (s *store) Record(_ context.Context, f domain.Finding) (domain.Finding, bool, error) {
	k := key(f)
	if existing, ok := s.held[k]; ok {
		next, err := existing.Seen(f.Invocation, f.Artifact, f.MappingID, f.LastSeen)
		if err != nil {
			return domain.Finding{}, false, err
		}
		s.held[k] = next
		return next, false, nil
	}
	s.held[k] = f
	return f, true, nil
}

func (s *store) ByID(_ context.Context, _, want id.ID) (domain.Finding, error) {
	for _, f := range s.held {
		if f.ID == want {
			return f, nil
		}
	}
	return domain.Finding{}, domain.ErrNotFound
}

func (s *store) Save(_ context.Context, f domain.Finding) error {
	s.held[key(f)] = f
	s.saved = append(s.saved, f)
	return nil
}

func (s *store) SaveDetail(_ context.Context, d domain.Detail) error {
	s.details = append(s.details, d)
	return nil
}

// known answers for the fragments it holds and `false` for everything else,
// which is the case 0041 skips rather than invents.
type known struct{ values map[string]id.ID }

func (k known) ForValue(_ context.Context, _ id.ID, kind, value string) (id.ID, bool, error) {
	got, ok := k.values[kind+"\x00"+value]
	return got, ok, nil
}

type direct struct{}

func (direct) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type minter struct{ n byte }

func (m *minter) NewID() id.ID {
	m.n++
	return an(0x80 + m.n)
}

type frozen struct{}

func (frozen) Now() time.Time { return at }

type recorder struct{ names []string }

func (r *recorder) Publish(_ context.Context, evs ...events.Event) error {
	for _, e := range evs {
		r.names = append(r.names, e.Name)
	}
	return nil
}

func (r *recorder) count(name string) int {
	n := 0
	for _, held := range r.names {
		if held == name {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------- the rig

func rig(t *testing.T) (*command.Findings, *store, *recorder) {
	t.Helper()
	repo := newStore()
	log := &recorder{}
	fragments := known{values: map[string]id.ID{
		"url\x00https://a.acme.test/": fragment,
	}}
	return command.NewFindings(repo, fragments, direct{}, log, &minter{}, frozen{}), repo, log
}

func sighting(signature, value, severity string) command.Sighting {
	return command.Sighting{
		WorkspaceID: space, ToolID: tool, InvocationID: an(0x20), ArtifactID: an(0x21),
		Signature: signature, SignatureMapping: an(0x22), Severity: severity,
		SubjectKind: "url", SubjectValue: value,
		Details: []command.Detail{{Field: "name", Value: "Log4j RCE", Mapping: an(0x23)}},
		SeenAt:  at,
	}
}

// ---------------------------------------------------------------- the claims

// 0041 §2: a sighting whose fragment does not exist is SKIPPED and counted. The
// `matched-at` value is normally a fragment the same extraction pass just made;
// when it is not — a scope rule kept the run off it, a fold mismatch — hanging
// the finding off a fragment invented here would put a problem on an asset
// nobody observed.
func TestASightingOnAnUnknownFragmentIsSkippedAndNotInvented(t *testing.T) {
	findings, repo, log := rig(t)
	got, err := findings.Record(context.Background(), []command.Sighting{
		sighting("CVE-2021-44228", "https://a.acme.test/", "critical"),
		sighting("CVE-2021-44228", "https://nobody-observed.test/", "critical"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Findings != 1 || got.Skipped != 1 {
		t.Fatalf("want 1 recorded and 1 skipped, got %+v", got)
	}
	if len(repo.held) != 1 {
		t.Fatalf("a finding was hung off an invented fragment: %d", len(repo.held))
	}
	if log.count(domain.EventFindingOpened) != 1 {
		t.Fatalf("one new problem, one event: %v", log.names)
	}
}

// An unrecognised severity is NOT filed as `info`. Quietly downgrading an
// unknown word would hide the loudest thing a scanner said, and `unknown` is not
// a level on 0004's scale.
func TestAnUnknownSeverityIsSkippedRatherThanDowngraded(t *testing.T) {
	findings, repo, _ := rig(t)
	got, err := findings.Record(context.Background(), []command.Sighting{
		sighting("CVE-2021-44228", "https://a.acme.test/", "catastrophic"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Skipped != 1 || got.Findings != 0 {
		t.Fatalf("want it skipped, got %+v", got)
	}
	if len(repo.held) != 0 {
		t.Fatal("an unknown severity must not become `info`")
	}
}

// `finding.opened` is NEWS and a rescan is a heartbeat. A subscriber that wants
// to page somebody wants only the first, and emitting one per sighting is how an
// alert becomes noise nobody reads.
func TestOnlyANewProblemIsAnnounced(t *testing.T) {
	findings, _, log := rig(t)
	one := []command.Sighting{sighting("CVE-2021-44228", "https://a.acme.test/", "critical")}

	first, err := findings.Record(context.Background(), one)
	if err != nil {
		t.Fatal(err)
	}
	if first.Opened != 1 {
		t.Fatalf("the first sighting opens it: %+v", first)
	}
	again, err := findings.Record(context.Background(), one)
	if err != nil {
		t.Fatal(err)
	}
	if again.Opened != 0 || again.Findings != 1 {
		t.Fatalf("a rescan is a sighting: %+v", again)
	}
	if log.count(domain.EventFindingOpened) != 1 {
		t.Fatalf("a rescan must not announce a new problem: %v", log.names)
	}
}

// Details are written per sighting and are UPSERTED by the store, so a nightly
// rescan refreshes the value rather than appending a row per night.
func TestDetailsAreWrittenForEverySighting(t *testing.T) {
	findings, repo, _ := rig(t)
	one := []command.Sighting{sighting("CVE-2021-44228", "https://a.acme.test/", "critical")}
	if _, err := findings.Record(context.Background(), one); err != nil {
		t.Fatal(err)
	}
	if len(repo.details) != 1 || repo.details[0].Field != "name" {
		t.Fatalf("details: %+v", repo.details)
	}
	if repo.details[0].ArtifactID != an(0x21) {
		t.Fatal("a detail cites the artifact it was read from")
	}
}

// **THERE IS NO PATH BACK TO `open`.** A finding reopens only by being SEEN
// again, which is a fact about the estate rather than somebody's opinion of it.
func TestNobodyCanReopenAFindingByHand(t *testing.T) {
	findings, repo, _ := rig(t)
	if _, err := findings.Record(context.Background(), []command.Sighting{
		sighting("CVE-2021-44228", "https://a.acme.test/", "critical"),
	}); err != nil {
		t.Fatal(err)
	}
	var held domain.Finding
	for _, f := range repo.held {
		held = f
	}
	if _, err := findings.Decide(context.Background(), space, held.ID, person,
		domain.StateOpen, ""); !errors.Is(err, domain.ErrStateUnknown) {
		t.Fatalf("want ErrStateUnknown, got %v", err)
	}
}

// A ruling is a DECISION — 0014 — and it emits one.
func TestRulingOnAFindingIsADecision(t *testing.T) {
	findings, repo, log := rig(t)
	if _, err := findings.Record(context.Background(), []command.Sighting{
		sighting("CVE-2021-44228", "https://a.acme.test/", "critical"),
	}); err != nil {
		t.Fatal(err)
	}
	var held domain.Finding
	for _, f := range repo.held {
		held = f
	}
	got, err := findings.Decide(context.Background(), space, held.ID, person,
		domain.StateDismissed, "the host is a honeypot")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != domain.StateDismissed || got.DecidedBy != person {
		t.Fatalf("%+v", got)
	}
	if log.count(domain.EventFindingDecided) != 1 {
		t.Fatalf("events: %v", log.names)
	}
	// AND A DISMISSAL WITHOUT A REASON IS REFUSED, through the command as well
	// as in the domain.
	if _, err := findings.Decide(context.Background(), space, held.ID, person,
		domain.StateDismissed, "  "); err == nil {
		t.Fatal("a dismissal says why it does not matter")
	}
}
