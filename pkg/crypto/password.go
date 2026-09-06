package crypto

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

const algorithm = "argon2id"

type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var Default = Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

func (p Params) validate() {
	switch {
	case p.Iterations < 1:
		panic("crypto: Iterations must be at least 1")
	case p.Parallelism < 1:
		panic("crypto: Parallelism must be at least 1")
	case p.Memory < 8*uint32(p.Parallelism):
		panic("crypto: Memory must be at least 8 KiB per lane")
	case p.SaltLength < 8:
		panic("crypto: SaltLength must be at least 8 bytes")
	case p.KeyLength < 16:
		panic("crypto: KeyLength must be at least 16 bytes")
	}
}

func (p Params) above(other Params) bool {
	return other.Memory < p.Memory ||
		other.Iterations < p.Iterations ||
		other.Parallelism < p.Parallelism ||
		other.SaltLength < p.SaltLength ||
		other.KeyLength < p.KeyLength
}

type Verification struct {
	Valid       bool
	NeedsRehash bool
}

type Hasher struct {
	params  Params
	entropy io.Reader
	dummy   string
}

func NewHasher(p Params, entropy io.Reader) *Hasher {
	p.validate()
	if entropy == nil {
		panic("crypto: NewHasher with a nil entropy source")
	}
	h := &Hasher{params: p, entropy: entropy}
	dummy, err := h.Hash("the account that does not exist")
	if err != nil {
		panic("crypto: the entropy source failed at construction: " + err.Error())
	}
	h.dummy = dummy
	return h
}

func (h *Hasher) Params() Params { return h.params }

func (h *Hasher) Dummy() string { return h.dummy }

func (h *Hasher) Hash(password string) (string, error) {
	if password == "" {
		return "", ErrPasswordEmpty
	}
	salt := make([]byte, h.params.SaltLength)
	if _, err := io.ReadFull(h.entropy, salt); err != nil {
		return "", errors.Wrap(err, errors.Internal, "crypto: read salt")
	}
	key := argon2.IDKey([]byte(password), salt,
		h.params.Iterations, h.params.Memory, h.params.Parallelism, h.params.KeyLength)
	return encode(h.params, salt, key), nil
}

func (h *Hasher) Verify(encoded, password string) (Verification, error) {
	params, salt, want, err := decode(encoded)
	if err != nil {
		return Verification{}, err
	}
	got := argon2.IDKey([]byte(password), salt,
		params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return Verification{}, nil
	}
	return Verification{Valid: true, NeedsRehash: h.params.above(params)}, nil
}

var b64 = base64.RawStdEncoding

func encode(p Params, salt, key []byte) string {
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		algorithm, argon2.Version, p.Memory, p.Iterations, p.Parallelism,
		b64.EncodeToString(salt), b64.EncodeToString(key))
}

func decode(encoded string) (Params, []byte, []byte, error) {
	bad := func(why string) (Params, []byte, []byte, error) {
		return Params{}, nil, nil, fmt.Errorf("crypto: %s: %w", why, ErrHashUnreadable)
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" {
		return bad("not PHC string format")
	}
	if parts[1] != algorithm {
		return bad("not produced by " + algorithm)
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return bad("no readable version")
	}
	if parts[2] != fmt.Sprintf("v=%d", version) {
		return bad("the version segment has trailing characters")
	}
	if version != argon2.Version {
		return bad(fmt.Sprintf("argon2 version %d, this build speaks %d", version, argon2.Version))
	}

	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d",
		&p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return bad("no readable parameters")
	}
	if parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", p.Memory, p.Iterations, p.Parallelism) {
		return bad("the parameter segment has trailing characters")
	}

	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return bad("the salt is not valid base64")
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil {
		return bad("the digest is not valid base64")
	}
	if len(salt) == 0 || len(key) == 0 {
		return bad("the salt or the digest is empty")
	}

	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(key))
	return p, salt, key, nil
}
