package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

const (
	MinPasswordLength = 12
	MaxPasswordLength = 1024
	SubjectKind       = "account"
)

var (
	ErrPasswordTooShort = errors.Newf(errors.Invalid,
		"a password must be at least %d characters", MinPasswordLength)
	ErrPasswordTooLong = errors.Newf(errors.Invalid,
		"a password must be at most %d characters", MaxPasswordLength)
)

type Registrar struct {
	repo      Repository
	tenancy   Provisioner
	tx        Transactor
	publisher events.Publisher
	hasher    Hasher
	ids       Minter
	clock     Clock
}

func NewRegistrar(
	repo Repository,
	tenancy Provisioner,
	tx Transactor,
	publisher events.Publisher,
	hasher Hasher,
	ids Minter,
	clock Clock,
) *Registrar {
	if repo == nil || tenancy == nil || tx == nil || publisher == nil ||
		hasher == nil || ids == nil || clock == nil {
		panic("identity: NewRegistrar with a nil dependency")
	}
	return &Registrar{
		repo: repo, tenancy: tenancy, tx: tx,
		publisher: publisher, hasher: hasher, ids: ids, clock: clock,
	}
}

type Registration struct {
	Email    string
	Password secret.String
	Name     string
}

type Registered struct {
	Account domain.Account
	Tenancy Tenancy
}

func (r *Registrar) Register(ctx context.Context, in Registration) (Registered, error) {
	email, err := domain.NewEmail(in.Email)
	if err != nil {
		return Registered{}, fmt.Errorf("identity: register: %w", err)
	}
	password := in.Password.Reveal()
	switch {
	case len(password) < MinPasswordLength:
		return Registered{}, ErrPasswordTooShort
	case len(password) > MaxPasswordLength:
		return Registered{}, ErrPasswordTooLong
	}

	hash, err := r.hasher.Hash(password)
	if err != nil {
		return Registered{}, fmt.Errorf("identity: register: %w", err)
	}

	at := r.clock.Now()
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name, _, _ = strings.Cut(email.String(), "@")
	}

	account, err := domain.NewAccount(r.ids.NewID(), email, name, at)
	if err != nil {
		return Registered{}, fmt.Errorf("identity: register: %w", err)
	}
	credential, err := domain.NewPassword(r.ids.NewID(), account.ID, hash, at)
	if err != nil {
		return Registered{}, fmt.Errorf("identity: register: %w", err)
	}

	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, r.ids)
	}
	created, err := events.NewDecision(r.ids, r.clock,
		domain.EventAccountCreated, SubjectKind+":"+account.ID.String(), prov,
		domain.AccountCreated{AccountID: account.ID.String(), Email: email.String()})
	if err != nil {
		return Registered{}, fmt.Errorf("identity: register: %w", err)
	}

	var tenancy Tenancy
	err = r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.repo.CreateAccount(ctx, account); err != nil {
			return err
		}
		if err := r.repo.CreateCredential(ctx, credential); err != nil {
			return err
		}
		t, err := r.tenancy.Provision(ctx, account.ID, name)
		if err != nil {
			return err
		}
		tenancy = t
		return r.publisher.Publish(ctx, created)
	})
	if err != nil {
		return Registered{}, fmt.Errorf("identity: register: %w", err)
	}
	return Registered{Account: account, Tenancy: tenancy}, nil
}
