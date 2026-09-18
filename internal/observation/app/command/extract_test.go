// Author-written, from decisions/0041 §2. The extractor is where a
// finding-producing tool is ROUTED away from the observation table, and the
// shape it hands `finding` is decided here and nowhere else.
package command_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/observation/app/command"
	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var at = time.Date(2026, 9, 8, 2, 0, 0, 0, time.UTC)

func an(b byte) id.ID {
	var out id.ID
	out[0] = b
	return out
}

const nucleiLine = `{"template-id":"CVE-2021-44228","matched-at":"https://a.acme.test/",` +
	`"info":{"name":"Log4j RCE","severity":"critical"},"type":"http"}`

const httpxLine = `{"url":"https://acme.test/","webserver":"nginx"}`

// ---------------------------------------------------------------- the fakes

type store struct {
	observations []domain.Observation
	unmapped     []domain.Unmapped
}

func (s *store) Create(_ context.Context, o domain.Observation) error {
	s.observations = append(s.observations, o)
	return nil
}

func (s *store) RecordUnmapped(_ context.Context, u domain.Unmapped) error {
	s.unmapped = append(s.unmapped, u)
	return nil
}

type live struct {
	mappings []domain.Mapping
	subject  string
	findings bool
}

func (l live) Live(context.Context, id.ID, id.ID) ([]domain.Mapping, string, bool, error) {
	return l.mappings, l.subject, l.findings, nil
}

type board struct{ got []command.Sighting }

func (b *board) Record(_ context.Context, in []command.Sighting) error {
	b.got = append(b.got, in...)
	return nil
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

func mapping(t *testing.T, n byte, field, expression string, role domain.Role) domain.Mapping {
	t.Helper()
	p, err := domain.ParsePath(expression)
	if err != nil {
		t.Fatal(err)
	}
	return domain.Mapping{ID: an(n), Field: field, Version: 1, Path: p, Role: role}
}

func run(t *testing.T, body string, l live) (*store, *board, command.Result) {
	t.Helper()
	repo := &store{}
	found := &board{}
	e := command.NewExtractor(repo, l, found, quiet{}, &minter{}, frozen{})
	got, err := e.Extract(context.Background(), command.Source{
		WorkspaceID: an(1), OrgID: an(2), InvocationID: an(3), ArtifactID: an(4),
		ToolID: an(5), Body: []byte(body), ObservedAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return repo, found, got
}

// ---------------------------------------------------------------- the claims

// 0041 §2: a finding-producing tool writes NO OBSERVATIONS. What it said is
// about a problem, not about the subject — filing `name: Log4j RCE` as an
// observation of the URL would put it in that asset's field list, where it reads
// as a property of the url.
func TestAFindingToolWritesFindingsAndNotObservations(t *testing.T) {
	repo, found, got := run(t, nucleiLine, live{
		subject: "url", findings: true,
		mappings: []domain.Mapping{
			mapping(t, 0x10, "matched", ".matched-at", domain.RoleSubject),
			mapping(t, 0x11, "template", ".template-id", domain.RoleSignature),
			mapping(t, 0x12, "level", ".info.severity", domain.RoleSeverity),
			mapping(t, 0x13, "name", ".info.name", domain.RoleAttribute),
		},
	})
	if len(repo.observations) != 0 {
		t.Fatalf("a finding tool must write no observations, got %d", len(repo.observations))
	}
	if len(found.got) != 1 {
		t.Fatalf("one record, one sighting: %d", len(found.got))
	}
	one := found.got[0]
	if one.Signature != "CVE-2021-44228" || one.Severity != "critical" {
		t.Fatalf("identity and severity: %+v", one)
	}
	if one.SubjectKind != "url" || one.SubjectValue != "https://a.acme.test/" {
		t.Fatalf("a finding is ON a fragment: %+v", one)
	}
	// **ONLY THE PLAIN ATTRIBUTES BECOME DETAILS.** The subject, the signature
	// and the severity are already columns, and repeating them would give a
	// reader two places to look and one to trust.
	if len(one.Details) != 1 || one.Details[0].Field != "name" {
		t.Fatalf("details must exclude the identity columns: %+v", one.Details)
	}
	// The FIELD ACCOUNTING still happened — a nuclei template that grew a field
	// nobody mapped is still an unmapped path, and the Extraction Quality panel
	// still says so.
	if got.FieldsSeen == 0 {
		t.Fatal("the field accounting is not skipped for a finding tool")
	}
	if len(repo.unmapped) == 0 {
		t.Fatal("`.type` was mapped by nobody and is still recorded")
	}
}

// Half an identity is not a finding. A record with no signature is dropped in
// the extractor rather than downstream, because 0041 §1 makes the signature the
// half that turns a rescan into a sighting.
func TestARecordWithNoSignatureIsNotAFinding(t *testing.T) {
	_, found, _ := run(t, `{"matched-at":"https://a.acme.test/","info":{"severity":"high"}}`,
		live{
			subject: "url", findings: true,
			mappings: []domain.Mapping{
				mapping(t, 0x10, "matched", ".matched-at", domain.RoleSubject),
				mapping(t, 0x11, "template", ".template-id", domain.RoleSignature),
				mapping(t, 0x12, "level", ".info.severity", domain.RoleSeverity),
			},
		})
	if len(found.got) != 0 {
		t.Fatalf("half an identity is not a finding: %+v", found.got)
	}
}

// And the ordinary path is untouched: a tool that produces anything else still
// writes observations and no findings.
func TestAnOrdinaryToolStillWritesObservations(t *testing.T) {
	repo, found, _ := run(t, httpxLine, live{
		subject: "url",
		mappings: []domain.Mapping{
			mapping(t, 0x10, "url", ".url", domain.RoleSubject),
			mapping(t, 0x11, "webserver", ".webserver", domain.RoleAttribute),
		},
	})
	if len(found.got) != 0 {
		t.Fatalf("an ordinary tool produces no findings: %+v", found.got)
	}
	if len(repo.observations) != 2 {
		t.Fatalf("two mapped fields are two observations, got %d", len(repo.observations))
	}
	// The ROLE travels onto the row — it is what `ProvenanceForInvocation`
	// filters on, so an observation that forgot it is a derivation that silently
	// never happens.
	for _, o := range repo.observations {
		if o.Field == "url" && o.Role != domain.RoleSubject {
			t.Fatalf("the subject reading records its role: %s", o.Role)
		}
	}
}

// A tool with no live mappings extracts NOTHING and that is not an error —
// nobody has taught this system to read it yet, which is where every tool starts.
func TestAToolWithNoMappingsExtractsNothing(t *testing.T) {
	repo, found, got := run(t, nucleiLine, live{subject: "url", findings: true})
	if len(repo.observations) != 0 || len(found.got) != 0 || got.Records != 0 {
		t.Fatalf("nothing is taught, nothing is read: %+v", got)
	}
}
