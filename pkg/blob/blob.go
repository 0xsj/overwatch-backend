package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
)

const Algo = "sha256"

var (
	ErrNotFound  = pkgerrors.New(pkgerrors.NotFound, "blob")
	ErrBadRef    = pkgerrors.New(pkgerrors.Invalid, "not a blob reference")
	ErrCorrupted = pkgerrors.New(pkgerrors.Internal, "the bytes on disk do not hash to their name")
)

// Ref names one blob. Algo is a field rather than an assumption so the day
// sha256 is not enough there is something to widen.
type Ref struct {
	Algo string
	Hex  string
}

func (r Ref) String() string { return r.Algo + ":" + r.Hex }

func (r Ref) IsZero() bool { return r.Hex == "" }

func ParseRef(s string) (Ref, error) {
	algo, hex, ok := strings.Cut(s, ":")
	if !ok || algo != Algo || len(hex) != 64 {
		return Ref{}, fmt.Errorf("blob: %q: %w", s, ErrBadRef)
	}
	for i := 0; i < len(hex); i++ {
		c := hex[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return Ref{}, fmt.Errorf("blob: %q: %w", s, ErrBadRef)
		}
	}
	return Ref{Algo: algo, Hex: hex}, nil
}

type Info struct {
	Ref  Ref
	Size int64
}

type Store struct {
	root string
}

func New(root string) (*Store, error) {
	if root == "" {
		return nil, pkgerrors.New(pkgerrors.Invalid, "blob: empty root")
	}
	if err := os.MkdirAll(filepath.Join(root, "tmp"), 0o750); err != nil {
		return nil, pkgerrors.Wrap(err, pkgerrors.Unavailable, "blob: prepare "+root)
	}
	return &Store{root: root}, nil
}

// Put streams r to disk, hashing as it goes, and renames the result into place.
// Storing the same bytes twice is one file and one answer.
func (s *Store) Put(ctx context.Context, r io.Reader) (Info, error) {
	tmp, err := os.CreateTemp(filepath.Join(s.root, "tmp"), "put-")
	if err != nil {
		return Info{}, pkgerrors.Wrap(err, pkgerrors.Unavailable, "blob: temp")
	}
	defer func() {
		tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	sum := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, sum), &cancellable{ctx: ctx, r: r})
	if err != nil {
		return Info{}, pkgerrors.Wrap(err, pkgerrors.Unavailable, "blob: write")
	}
	if err := tmp.Close(); err != nil {
		return Info{}, pkgerrors.Wrap(err, pkgerrors.Unavailable, "blob: close")
	}

	ref := Ref{Algo: Algo, Hex: hex.EncodeToString(sum.Sum(nil))}
	final := s.path(ref)
	if err := os.MkdirAll(filepath.Dir(final), 0o750); err != nil {
		return Info{}, pkgerrors.Wrap(err, pkgerrors.Unavailable, "blob: mkdir")
	}
	// Already there means the same bytes: the name IS the hash. Rewriting would
	// churn the disk to produce a file identical to the one already present.
	if _, err := os.Stat(final); err == nil {
		return Info{Ref: ref, Size: size}, nil
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return Info{}, pkgerrors.Wrap(err, pkgerrors.Unavailable, "blob: place")
	}
	return Info{Ref: ref, Size: size}, nil
}

func (s *Store) Open(_ context.Context, ref Ref) (io.ReadCloser, error) {
	if ref.IsZero() {
		return nil, fmt.Errorf("blob: %w", ErrBadRef)
	}
	f, err := os.Open(s.path(ref))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("blob: %s: %w", ref, ErrNotFound)
	}
	if err != nil {
		return nil, pkgerrors.Wrap(err, pkgerrors.Unavailable, "blob: open "+ref.String())
	}
	return f, nil
}

func (s *Store) Stat(_ context.Context, ref Ref) (Info, error) {
	if ref.IsZero() {
		return Info{}, fmt.Errorf("blob: %w", ErrBadRef)
	}
	fi, err := os.Stat(s.path(ref))
	if errors.Is(err, os.ErrNotExist) {
		return Info{}, fmt.Errorf("blob: %s: %w", ref, ErrNotFound)
	}
	if err != nil {
		return Info{}, pkgerrors.Wrap(err, pkgerrors.Unavailable, "blob: stat "+ref.String())
	}
	return Info{Ref: ref, Size: fi.Size()}, nil
}

// Verify re-hashes what is on disk. A record citing bytes that no longer hash to
// their name is a record that stopped being true, and only reading them says so.
func (s *Store) Verify(ctx context.Context, ref Ref) error {
	r, err := s.Open(ctx, ref)
	if err != nil {
		return err
	}
	defer r.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, r); err != nil {
		return pkgerrors.Wrap(err, pkgerrors.Unavailable, "blob: read "+ref.String())
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != ref.Hex {
		return fmt.Errorf("blob: %s is now %s: %w", ref, got, ErrCorrupted)
	}
	return nil
}

func (s *Store) path(ref Ref) string {
	return filepath.Join(s.root, ref.Algo, ref.Hex[0:2], ref.Hex[2:4], ref.Hex)
}

// cancellable makes a long copy answer a cancelled context. io.Copy has no
// context, so the check goes on the read side where the loop already is.
type cancellable struct {
	ctx context.Context
	r   io.Reader
}

func (c *cancellable) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
