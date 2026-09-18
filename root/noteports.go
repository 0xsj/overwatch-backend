package root

import (
	"context"

	notequery "github.com/0xsj/overwatch-backend/internal/note/app/query"
	reportdomain "github.com/0xsj/overwatch-backend/internal/report/domain"
	scopedomain "github.com/0xsj/overwatch-backend/internal/scope/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// knownKinds is `note`'s port into `scope`, which holds the canonical kind
// vocabulary — `0034`.
//
// **It asks rather than copying.** Five domains already keep a copy of those
// fourteen words and `root/vocabulary_test.go` enforces three of them; a sixth
// copy would be a sixth thing to drift, and this one costs a function.
type knownKinds struct{}

func (knownKinds) Known(kind string) bool {
	_, err := scopedomain.ParseKind(kind)
	return err == nil
}

// engagementNotes is `report`'s eighth section — decisions/0043 §3.
//
// **It carries the SUBJECTLESS notes only.** A note about one host belongs on
// that host's drawer; the engagement summary is what a client's report is for,
// and mixing them would put "this is a staging box, ignore it" into a document
// somebody sends a client.
type engagementNotes struct{ notes *notequery.Notes }

func (e engagementNotes) Notes(ctx context.Context, workspace, target id.ID) ([]reportdomain.Note, error) {
	found, err := e.notes.AllSummary(ctx, workspace)
	if err != nil {
		return nil, err
	}
	out := make([]reportdomain.Note, 0, len(found))
	for _, one := range found {
		out = append(out, reportdomain.Note{
			Body: one.Body, Author: one.Author.String(),
			At: date(one.CreatedAt), Edited: one.Edited(),
		})
	}
	return out, nil
}
