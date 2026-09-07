package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/tool/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Mappings writes versions. **There is no Edit** — an observation cites the
// version that produced it, so correcting a mapping is adding one.
type Mappings struct {
	repo      Repository
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewMappings(repo Repository, tx Transactor, publisher events.Publisher,
	ids Minter, clock Clock) *Mappings {
	if repo == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("tool: NewMappings with a nil dependency")
	}
	return &Mappings{repo: repo, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

// Draft adds a version without making it live. `promote` folds the promotion in
// — the common case is a person fixing a parser and wanting the fix to take, and
// making them call twice invites a corrected mapping that nothing runs.
//
// **This is what `correction` is.** It gets no table: a correction is a version
// whose author is a person, and the author is a field.
func (m *Mappings) Draft(ctx context.Context, org, tool, by id.ID,
	field, expression string, promote bool) (domain.Mapping, error) {
	held, err := m.repo.ByID(ctx, org, tool)
	if err != nil {
		return domain.Mapping{}, err
	}
	if held.Archived() {
		return domain.Mapping{}, domain.ErrArchived
	}

	var written domain.Mapping
	err = m.tx.InTx(ctx, func(ctx context.Context) error {
		version, err := m.repo.NextVersion(ctx, tool, field)
		if err != nil {
			return err
		}
		fresh, err := domain.NewMapping(m.ids.NewID(), org, tool, by,
			field, expression, version, m.clock.Now())
		if err != nil {
			return err
		}
		if err := m.repo.CreateMapping(ctx, fresh); err != nil {
			return err
		}
		written = fresh
		if !promote {
			return nil
		}
		written, _, err = m.promote(ctx, fresh)
		return err
	})
	if err != nil {
		return domain.Mapping{}, err
	}

	if err := m.emit(ctx, domain.EventMappingAdded, org, domain.MappingAdded{
		MappingID: written.ID.String(), ToolID: tool.String(), OrgID: org.String(),
		Field: written.Field, Version: written.Version, ByPerson: !by.IsZero(),
	}); err != nil {
		return domain.Mapping{}, err
	}
	if promote {
		return written, m.emit(ctx, domain.EventMappingLive, org, domain.MappingPromoted{
			MappingID: written.ID.String(), ToolID: tool.String(), OrgID: org.String(),
			Field: written.Field, To: written.Version,
		})
	}
	return written, nil
}

// Promote makes an existing draft the version that runs.
func (m *Mappings) Promote(ctx context.Context, org, want id.ID) (domain.Mapping, error) {
	held, err := m.repo.MappingByID(ctx, org, want)
	if err != nil {
		return domain.Mapping{}, err
	}
	var (
		live domain.Mapping
		from int
	)
	err = m.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		live, from, err = m.promote(ctx, held)
		return err
	})
	if err != nil {
		return domain.Mapping{}, err
	}
	return live, m.emit(ctx, domain.EventMappingLive, org, domain.MappingPromoted{
		MappingID: live.ID.String(), ToolID: live.ToolID.String(), OrgID: org.String(),
		Field: live.Field, From: from, To: live.Version,
	})
}

// promote runs inside the caller's transaction and does the two writes in the
// only order the index permits: RETIRE the incumbent, then promote. Reversed,
// the second write is refused by `mapping_live` and the field keeps the old
// version while the caller is told it succeeded.
//
// It answers with the version it displaced, so the event can name both without
// a second read — zero when there was none.
func (m *Mappings) promote(ctx context.Context, want domain.Mapping) (domain.Mapping, int, error) {
	from := 0
	incumbent, err := m.repo.LiveMapping(ctx, want.ToolID, want.Field)
	switch {
	case err == nil:
		if incumbent.ID == want.ID {
			return want, 0, domain.ErrAlreadyLive
		}
		from = incumbent.Version
		retired, err := incumbent.Retire(m.clock.Now())
		if err != nil {
			return domain.Mapping{}, 0, err
		}
		if err := m.repo.SaveMapping(ctx, retired); err != nil {
			return domain.Mapping{}, 0, err
		}
	case errors.Is(err, domain.ErrMappingGone):
		// No live version for this field yet. The first promotion, or the
		// silence a crashed one left behind — both are this case.
	default:
		return domain.Mapping{}, 0, err
	}

	live, err := want.Promote(m.clock.Now())
	if err != nil {
		return domain.Mapping{}, 0, err
	}
	if err := m.repo.SaveMapping(ctx, live); err != nil {
		return domain.Mapping{}, 0, err
	}
	return live, from, nil
}

func (m *Mappings) emit(ctx context.Context, name string, org id.ID, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, m.ids)
	}
	e, err := events.NewDecision(m.ids, m.clock, name,
		domain.SubjectKind+":"+org.String(), prov, payload)
	if err != nil {
		return fmt.Errorf("tool: %s: %w", name, err)
	}
	if err := m.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("tool: %s: %w", name, err)
	}
	return nil
}
