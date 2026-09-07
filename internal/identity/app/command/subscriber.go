package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Subscriber sends the first verification link, by listening for the account it
// belongs to rather than being called by the code that created it.
//
// Registration therefore does not know a mail server exists — the same shape
// org and workspace already have (decisions/0017), and for the stronger reason:
// a mail server that is down would otherwise fail a registration that
// succeeded, and rolling the account back because the SMTP handshake timed out
// loses a person's password for an outage they cannot see.
type Subscriber struct {
	verifier *Verifier
	ids      Minter
}

func NewSubscriber(verifier *Verifier, ids Minter) *Subscriber {
	if verifier == nil || ids == nil {
		panic("identity: NewSubscriber with a nil dependency")
	}
	return &Subscriber{verifier: verifier, ids: ids}
}

// Handle mails a fresh link for every delivery of identity.account.created.
//
// **A redelivery sends a second mail, and that is accepted rather than
// deduplicated.** Delivery is at-least-once (decisions/0007), and the second
// link invalidates the first — which is precisely what happens when a person
// presses "resend". The failure it trades against, suppressing the mail because
// a row says one was already sent, leaves somebody with no link at all and no
// way to tell.
func (s *Subscriber) Handle(ctx context.Context, e events.Event) error {
	if e.Name != domain.EventAccountCreated {
		return nil
	}
	account, err := id.Parse(e.SubjectID())
	if err != nil {
		return fmt.Errorf("identity: %s has no readable subject %q: %w", e.Name, e.Subject, err)
	}
	if e.Provenance.IsZero() {
		return fmt.Errorf("identity: %s carries no provenance", e.Name)
	}
	prov, err := e.Provenance.DeriveFrom(s.ids, e.ID)
	if err != nil {
		return err
	}
	return s.verifier.IssueVerification(provenance.NewContext(ctx, prov), account)
}
