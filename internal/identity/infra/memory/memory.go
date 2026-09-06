package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Store struct {
	mu          sync.Mutex
	accounts    map[id.ID]domain.Account
	credentials map[id.ID]domain.Credential
	sessions    map[id.ID]domain.Session
	depth       int
}

func New() *Store {
	return &Store{
		accounts:    map[id.ID]domain.Account{},
		credentials: map[id.ID]domain.Credential{},
		sessions:    map[id.ID]domain.Session{},
	}
}

func (s *Store) InTx(ctx context.Context, fn func(context.Context) error) (err error) {
	s.mu.Lock()
	if s.depth > 0 {
		s.depth++
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.depth--
			s.mu.Unlock()
		}()
		return fn(ctx)
	}
	accounts := clone(s.accounts)
	credentials := clone(s.credentials)
	sessions := clone(s.sessions)
	s.depth = 1
	s.mu.Unlock()

	restore := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.accounts, s.credentials, s.sessions = accounts, credentials, sessions
		s.depth = 0
	}
	commit := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.depth = 0
	}

	defer func() {
		switch p := recover(); {
		case p != nil:
			restore()
			panic(p)
		case err != nil:
			restore()
		default:
			commit()
		}
	}()

	return fn(ctx)
}

func clone[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Store) CreateAccount(_ context.Context, a domain.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.accounts {
		if existing.Email == a.Email && existing.Status != domain.StatusArchived {
			return fmt.Errorf("identity: insert account: %w", domain.ErrAccountExists)
		}
	}
	s.accounts[a.ID] = a
	return nil
}

func (s *Store) AccountByID(_ context.Context, want id.ID) (domain.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accounts[want]
	if !ok {
		return domain.Account{}, fmt.Errorf("identity: read account: %w", domain.ErrAccountNotFound)
	}
	return a, nil
}

func (s *Store) AccountByEmail(_ context.Context, want domain.Email) (domain.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.accounts {
		if a.Email == want && a.Status != domain.StatusArchived {
			return a, nil
		}
	}
	return domain.Account{}, fmt.Errorf("identity: read account by email: %w", domain.ErrAccountNotFound)
}

func (s *Store) SaveAccount(_ context.Context, a domain.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.accounts[a.ID]
	if !ok {
		return fmt.Errorf("identity: update account: %w", domain.ErrAccountNotFound)
	}
	if existing.Version != a.Version-1 {
		return fmt.Errorf("identity: update account: %w", domain.ErrStaleWrite)
	}
	s.accounts[a.ID] = a
	return nil
}

func (s *Store) CreateCredential(_ context.Context, c domain.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.credentials {
		if existing.Hash == c.Hash {
			return fmt.Errorf("identity: insert credential: %w", domain.ErrAlreadyRevoked)
		}
		if c.Kind == domain.KindPassword && existing.Kind == domain.KindPassword &&
			existing.AccountID == c.AccountID && !existing.Revoked() {
			return fmt.Errorf("identity: insert credential: %w", domain.ErrAlreadyRevoked)
		}
	}
	s.credentials[c.ID] = c
	return nil
}

func (s *Store) CredentialByID(_ context.Context, want id.ID) (domain.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.credentials[want]
	if !ok {
		return domain.Credential{}, fmt.Errorf("identity: read credential: %w", domain.ErrCredentialGone)
	}
	return c, nil
}

func (s *Store) LivePasswordFor(_ context.Context, account id.ID) (domain.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.credentials {
		if c.AccountID == account && c.Kind == domain.KindPassword && !c.Revoked() {
			return c, nil
		}
	}
	return domain.Credential{}, fmt.Errorf("identity: read password: %w", domain.ErrCredentialGone)
}

func (s *Store) SaveCredential(_ context.Context, c domain.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.credentials[c.ID]
	if !ok {
		return fmt.Errorf("identity: update credential: %w", domain.ErrCredentialGone)
	}
	if existing.Version != c.Version-1 {
		return fmt.Errorf("identity: update credential: %w", domain.ErrStaleWrite)
	}
	s.credentials[c.ID] = c
	return nil
}
