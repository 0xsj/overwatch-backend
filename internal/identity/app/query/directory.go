package query

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// DirectoryReader is its own port and shares nothing with [Reader] next door.
// Naming people is a different question from authenticating one, and the
// overlap people expect — "surely both need AccountByID" — is where two
// unrelated screens start constraining each other.
type DirectoryReader interface {
	AccountByID(ctx context.Context, account id.ID) (domain.Account, error)
}

// Person is what another domain's screen needs to render a human: enough to
// recognise them, and nothing about how they authenticate.
type Person struct {
	AccountID id.ID
	Email     domain.Email
	Name      string
	Status    domain.Status
}

type Directory struct{ reader DirectoryReader }

func NewDirectory(reader DirectoryReader) *Directory {
	if reader == nil {
		panic("identity: NewDirectory with a nil reader")
	}
	return &Directory{reader: reader}
}

// Named resolves ids to people, and is how a members list gets its emails
// without org learning that identity has tables.
//
// **An id that resolves to nothing is OMITTED, not an error.** The caller is
// holding ids from another domain's rows and this package cannot vouch for
// them; failing the whole list because one account was archived away turns a
// rendering problem into an outage. The caller sees a shorter map and can say
// so.
func (d *Directory) Named(ctx context.Context, accounts []id.ID) (map[id.ID]Person, error) {
	out := make(map[id.ID]Person, len(accounts))
	for _, want := range accounts {
		if want.IsZero() {
			continue
		}
		if _, seen := out[want]; seen {
			continue
		}
		account, err := d.reader.AccountByID(ctx, want)
		if err != nil {
			if errors.Is(err, domain.ErrAccountNotFound) {
				continue
			}
			return nil, fmt.Errorf("identity: directory: %w", err)
		}
		out[account.ID] = Person{
			AccountID: account.ID,
			Email:     account.Email,
			Name:      account.Name,
			Status:    account.Status,
		}
	}
	return out, nil
}
