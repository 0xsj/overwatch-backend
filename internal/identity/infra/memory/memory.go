package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Store struct {
	mu          sync.Mutex
	accounts    map[id.ID]domain.Account
	credentials map[id.ID]domain.Credential
	sessions    map[id.ID]domain.Session
	tokens      map[id.ID]domain.Token
	depth       int
}

func New() *Store {
	return &Store{
		accounts:    map[id.ID]domain.Account{},
		credentials: map[id.ID]domain.Credential{},
		sessions:    map[id.ID]domain.Session{},
		tokens:      map[id.ID]domain.Token{},
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
	tokens := clone(s.tokens)
	s.depth = 1
	s.mu.Unlock()

	restore := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.accounts, s.credentials, s.sessions, s.tokens = accounts, credentials, sessions, tokens
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

func (s *Store) CreateSession(_ context.Context, sn domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.sessions {
		if existing.Hash == sn.Hash {
			return fmt.Errorf("identity: insert session: %w", domain.ErrAlreadyRevoked)
		}
	}
	s.sessions[sn.ID] = sn
	return nil
}

func (s *Store) SessionByHash(_ context.Context, hash string) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sn := range s.sessions {
		if sn.Hash == hash {
			return sn, nil
		}
	}
	return domain.Session{}, fmt.Errorf("identity: read session: %w", domain.ErrSessionGone)
}

func (s *Store) SessionsFor(_ context.Context, account id.ID) ([]domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Session, 0, len(s.sessions))
	for _, sn := range s.sessions {
		if sn.AccountID == account {
			out = append(out, sn)
		}
	}
	return out, nil
}

func (s *Store) EndSession(_ context.Context, want id.ID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sn, ok := s.sessions[want]
	if !ok || sn.Revoked() {
		return fmt.Errorf("identity: revoke session: %w", domain.ErrSessionGone)
	}
	next, err := sn.Revoke(at)
	if err != nil {
		return err
	}
	s.sessions[want] = next
	return nil
}

func (s *Store) EndSessionsFor(_ context.Context, account id.ID, at time.Time) ([]id.ID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var revoked []id.ID
	for k, sn := range s.sessions {
		if sn.AccountID != account || sn.Revoked() {
			continue
		}
		next, err := sn.Revoke(at)
		if err != nil {
			return revoked, err
		}
		s.sessions[k] = next
		revoked = append(revoked, next.ID)
	}
	return revoked, nil
}

func (s *Store) CreateToken(_ context.Context, t domain.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.tokens {
		if existing.Hash == t.Hash {
			return fmt.Errorf("identity: insert token: %w", domain.ErrTokenSpent)
		}
	}
	s.tokens[t.ID] = t
	return nil
}

func (s *Store) TokenByHash(_ context.Context, hash string) (domain.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tokens {
		if t.Hash == hash {
			return t, nil
		}
	}
	return domain.Token{}, fmt.Errorf("identity: read token: %w", domain.ErrTokenGone)
}

func (s *Store) ConsumeToken(_ context.Context, want id.ID, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[want]
	if !ok || t.Consumed() {
		return fmt.Errorf("identity: consume token: %w", domain.ErrTokenSpent)
	}
	next, err := t.Consume(at)
	if err != nil {
		return err
	}
	s.tokens[want] = next
	return nil
}

func (s *Store) ConsumeLiveTokens(_ context.Context, account id.ID, kind domain.TokenKind, at time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k, t := range s.tokens {
		if t.AccountID != account || t.Kind != kind || t.Consumed() {
			continue
		}
		next, err := t.Consume(at)
		if err != nil {
			continue
		}
		s.tokens[k] = next
		n++
	}
	return n, nil
}

func (s *Store) LiveSessionsFor(_ context.Context, account id.ID, at time.Time) ([]domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Session
	for _, sn := range s.sessions {
		if sn.AccountID == account && sn.Live(at) {
			out = append(out, sn)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IssuedAt.After(out[j].IssuedAt) })
	return out, nil
}

func (s *Store) EndSessionsExcept(_ context.Context, account, keep id.ID, at time.Time) ([]id.ID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var revoked []id.ID
	for k, sn := range s.sessions {
		if sn.AccountID != account || sn.ID == keep || sn.Revoked() {
			continue
		}
		next, err := sn.Revoke(at)
		if err != nil {
			return revoked, err
		}
		s.sessions[k] = next
		revoked = append(revoked, next.ID)
	}
	return revoked, nil
}

func (s *Store) CredentialsFor(_ context.Context, account id.ID) ([]domain.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Credential
	for _, c := range s.credentials {
		if c.AccountID == account {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
