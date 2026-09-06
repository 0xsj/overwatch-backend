package crypto

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"io"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

const TokenBytes = 32

type Token struct {
	Plaintext secret.String
	Hash      string
}

type Minter struct {
	entropy io.Reader
}

func NewMinter(entropy io.Reader) *Minter {
	if entropy == nil {
		panic("crypto: NewMinter with a nil entropy source")
	}
	return &Minter{entropy: entropy}
}

func (m *Minter) New() (Token, error) {
	raw := make([]byte, TokenBytes)
	if _, err := io.ReadFull(m.entropy, raw); err != nil {
		return Token{}, errors.Wrap(err, errors.Internal, "crypto: read token")
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)
	return Token{Plaintext: secret.New(plaintext), Hash: HashToken(plaintext)}, nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func EqualToken(hash, token string) bool {
	return subtle.ConstantTimeCompare([]byte(hash), []byte(HashToken(token))) == 1
}
