// Package crypto_test is a specification-derived test suite for
// github.com/0xsj/overwatch-backend/pkg/crypto. It was written against the
// package's godoc only, without sight of the implementation. Every assertion
// below is traceable to a sentence in that documentation; where the
// documentation was silent the test was either omitted or explicitly marked
// as an inference at the point it is made.
package crypto_test

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
)

// --- test helpers -----------------------------------------------------

// cheapParams is deliberately far below crypto.Default so tests that only
// need *a* valid hash, not specifically a default-cost one, stay fast.
// The spec states Params is a constructor argument, so choosing our own is
// within the contract.
func cheapParams() crypto.Params {
	return crypto.Params{
		Memory:      8 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// constantByteReader is a deterministic, stateless entropy source: every
// Read fills the buffer with the same byte, no matter how many times or how
// much was read before. Used to pin salt-driven randomness so a hash
// becomes a pure function of (password, params, salt) for a determinism
// test.
type constantByteReader struct {
	b byte
}

func (r constantByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
	}
	return len(p), nil
}

// countingReader wraps a real entropy source and records the total number
// of bytes it has handed out.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// failingReader always errors, simulating an entropy source that is simply
// unavailable.
type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

// shortReader serves exactly `remaining` bytes across however many Read
// calls it takes, then reports io.EOF forever after, simulating an entropy
// source that runs out partway through.
type shortReader struct {
	remaining int
}

func newShortReader(n int) *shortReader { return &shortReader{remaining: n} }

func (s *shortReader) Read(p []byte) (int, error) {
	if s.remaining <= 0 {
		return 0, io.EOF
	}
	n := s.remaining
	if n > len(p) {
		n = len(p)
	}
	for i := 0; i < n; i++ {
		p[i] = 0x01
	}
	s.remaining -= n
	return n, nil
}

const samplePassword = "correct horse battery staple"

// --- Default and the PHC format ----------------------------------------

func TestSpecDefaultMatchesDocumentedRFC9106Values(t *testing.T) {
	want := crypto.Params{
		Memory:      64 * 1024,
		Iterations:  3,
		Parallelism: 4,
		SaltLength:  16,
		KeyLength:   32,
	}
	if crypto.Default != want {
		t.Errorf("crypto.Default = %+v, want %+v — the doc states these exact values as RFC 9106 §4's second recommended option, and citing the RFC beats inventing numbers", crypto.Default, want)
	}
}

func TestSpecHashProducesPHCFormat(t *testing.T) {
	cases := []struct {
		name   string
		params crypto.Params
	}{
		{"default parameters", crypto.Default},
		{"custom, cheap parameters", cheapParams()},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := crypto.NewHasher(c.params, rand.Reader)
			encoded, err := h.Hash(samplePassword)
			if err != nil {
				t.Fatalf("Hash returned an unexpected error: %v", err)
			}

			fields := strings.Split(encoded, "$")
			if len(fields) != 6 {
				t.Fatalf("expected the six dollar-delimited segments of $argon2id$v=19$m=...,t=...,p=...$<salt>$<hash>, got %d in %q", len(fields), encoded)
			}
			if fields[0] != "" {
				t.Errorf("a PHC string begins with a leading '$', so the first split segment must be empty; got %q", fields[0])
			}
			if fields[1] != "argon2id" {
				t.Errorf("algorithm segment = %q, want %q — the spec states Hasher is argon2id", fields[1], "argon2id")
			}
			if fields[2] != "v=19" {
				t.Errorf("version segment = %q, want %q", fields[2], "v=19")
			}
			wantParams := fmt.Sprintf("m=%d,t=%d,p=%d", c.params.Memory, c.params.Iterations, c.params.Parallelism)
			if fields[3] != wantParams {
				t.Errorf("cost-parameter segment = %q, want %q — the parameters used to hash must travel with the digest so they can later be raised without invalidating it", fields[3], wantParams)
			}
			if fields[4] == "" {
				t.Errorf("salt segment must not be empty")
			}
			if fields[5] == "" {
				t.Errorf("hash segment must not be empty")
			}
		})
	}
}

func TestSpecHasherParamsRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		params crypto.Params
	}{
		{"default parameters", crypto.Default},
		{"custom parameters", cheapParams()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := crypto.NewHasher(c.params, rand.Reader)
			if got := h.Params(); got != c.params {
				t.Errorf("Params() = %+v, want the exact Params the Hasher was constructed with (%+v)", got, c.params)
			}
		})
	}
}

func TestSpecParamsControlEncodedSegmentLength(t *testing.T) {
	t.Run("a larger SaltLength yields a longer salt segment", func(t *testing.T) {
		small := crypto.NewHasher(crypto.Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 8, KeyLength: 32}, rand.Reader)
		big := crypto.NewHasher(crypto.Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 32, KeyLength: 32}, rand.Reader)

		smallEncoded, err := small.Hash(samplePassword)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		bigEncoded, err := big.Hash(samplePassword)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}

		smallSalt := strings.Split(smallEncoded, "$")[4]
		bigSalt := strings.Split(bigEncoded, "$")[4]

		if len(bigSalt) <= len(smallSalt) {
			t.Errorf("a Hasher configured with a larger SaltLength must produce a longer encoded salt segment (got %d chars vs %d chars for a smaller SaltLength) — SaltLength is meaningless if it does not change what is stored", len(bigSalt), len(smallSalt))
		}
	})

	t.Run("a larger KeyLength yields a longer digest segment", func(t *testing.T) {
		small := crypto.NewHasher(crypto.Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16}, rand.Reader)
		big := crypto.NewHasher(crypto.Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 64}, rand.Reader)

		smallEncoded, err := small.Hash(samplePassword)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		bigEncoded, err := big.Hash(samplePassword)
		if err != nil {
			t.Fatalf("setup: %v", err)
		}

		smallDigest := strings.Split(smallEncoded, "$")[5]
		bigDigest := strings.Split(bigEncoded, "$")[5]

		if len(bigDigest) <= len(smallDigest) {
			t.Errorf("a Hasher configured with a larger KeyLength must produce a longer encoded digest segment (got %d chars vs %d chars for a smaller KeyLength)", len(bigDigest), len(smallDigest))
		}
	})
}

// --- determinism vs. non-determinism ------------------------------------

func TestSpecHashIsNonDeterministicWithRandomSalt(t *testing.T) {
	h := crypto.NewHasher(cheapParams(), rand.Reader)

	a, err := h.Hash(samplePassword)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	b, err := h.Hash(samplePassword)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if a == b {
		t.Errorf("hashing the same password twice produced identical output; a fresh random salt must make every hash different, or two accounts sharing a password would be visibly identical in the database")
	}

	for _, encoded := range []string{a, b} {
		v, err := h.Verify(encoded, samplePassword)
		if err != nil {
			t.Fatalf("Verify returned an unexpected error for a hash this Hasher just produced: %v", err)
		}
		if !v.Valid {
			t.Errorf("a hash produced by Hash() must verify against the password it was made from")
		}
	}
}

func TestSpecHashIsDeterministicGivenFixedEntropy(t *testing.T) {
	// Argon2id must be a pure function of (password, salt, params): the same
	// inputs produce the same digest. Pinning the salt by fixing the
	// entropy source isolates that claim from the salt's own randomness.
	params := cheapParams()

	h1 := crypto.NewHasher(params, constantByteReader{b: 0x42})
	h2 := crypto.NewHasher(params, constantByteReader{b: 0x42})

	a, err := h1.Hash(samplePassword)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	b, err := h2.Hash(samplePassword)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	if a != b {
		t.Errorf("with the same password, the same Params, and a salt drawn from an identical entropy source, Hash must be pure: got %q and %q", a, b)
	}
}

// --- empty password / password policy -----------------------------------

func TestSpecHashRejectsEmptyPassword(t *testing.T) {
	h := crypto.NewHasher(cheapParams(), rand.Reader)
	encoded, err := h.Hash("")
	if err == nil {
		t.Fatalf(`Hash("") succeeded; an empty password has nothing to be expensive about testing and must be refused`)
	}
	if !errors.Is(err, crypto.ErrPasswordEmpty) {
		t.Errorf("expected ErrPasswordEmpty, got %v", err)
	}
	// Inference: a Go function returning (T, error) conventionally returns
	// the zero value of T alongside a non-nil error. The spec does not
	// state this explicitly for Hash, but it is the ordinary contract.
	if encoded != "" {
		t.Errorf("Hash(\"\") returned a non-empty string (%q) alongside an error", encoded)
	}
}

func TestSpecHashAcceptsAnyNonEmptyPassword(t *testing.T) {
	h := crypto.NewHasher(cheapParams(), rand.Reader)
	cases := []struct {
		name string
		pw   string
	}{
		{"a single whitespace character", " "},
		{"a very long password", strings.Repeat("a", 5000)},
		{"a password containing unicode", "пароль🔒密码"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := h.Hash(c.pw); err != nil {
				t.Errorf("Hash rejected a non-empty password: %v — the doc states this package refuses an empty password and nothing else", err)
			}
		})
	}
}

// --- verification round trip ---------------------------------------------

func TestSpecVerifyRoundTrip(t *testing.T) {
	h := crypto.NewHasher(cheapParams(), rand.Reader)
	cases := []struct {
		name string
		pw   string
	}{
		{"an ordinary password", samplePassword},
		{"a password that is only whitespace", " "},
		{"a very long password", strings.Repeat("z", 500)},
		{"a password containing unicode", "пароль🔒"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			encoded, err := h.Hash(c.pw)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}

			t.Run("the correct password verifies", func(t *testing.T) {
				v, err := h.Verify(encoded, c.pw)
				if err != nil {
					t.Fatalf("Verify returned an unexpected error for the correct password: %v", err)
				}
				if !v.Valid {
					t.Errorf("the correct password must verify")
				}
			})

			t.Run("a different password fails without being treated as an error", func(t *testing.T) {
				v, err := h.Verify(encoded, c.pw+"x")
				if err != nil {
					t.Errorf("a wrong password is not a malformed hash and must not produce an error; got %v", err)
				}
				if v.Valid {
					t.Errorf("an incorrect password must not verify")
				}
			})
		})
	}
}

// --- NeedsRehash reflects policy, not mere difference ---------------------

func TestSpecNeedsRehashReflectsPolicyNotDifference(t *testing.T) {
	weak := crypto.Params{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	strong := crypto.Default // strictly more memory, iterations and parallelism than weak

	hWeak := crypto.NewHasher(weak, rand.Reader)
	hStrong := crypto.NewHasher(strong, rand.Reader)

	weakHash, err := hWeak.Hash(samplePassword)
	if err != nil {
		t.Fatalf("setup: hashing with weak params: %v", err)
	}
	strongHash, err := hStrong.Hash(samplePassword)
	if err != nil {
		t.Fatalf("setup: hashing with strong (default) params: %v", err)
	}

	cases := []struct {
		name            string
		hasher          *crypto.Hasher
		hash            string
		wantNeedsRehash bool
		reason          string
	}{
		{
			"a weak hash checked against its own weak policy does not need rehashing",
			hWeak, weakHash, false,
			"a hash already at the policy checking it is not below that policy",
		},
		{
			"a weak hash checked against a stronger policy needs rehashing",
			hStrong, weakHash, true,
			"NeedsRehash must fire when the embedded parameters are below current policy, which is exactly the moment the plaintext is available to fix it",
		},
		{
			"a strong hash checked against its own strong policy does not need rehashing",
			hStrong, strongHash, false,
			"a hash already at the policy checking it is not below that policy",
		},
		{
			"a strong hash checked against a weaker policy does not need rehashing",
			hWeak, strongHash, false,
			"the doc states NeedsRehash reports parameters below current policy, not merely different from it — a hash stronger than necessary must never be asked to downgrade",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := c.hasher.Verify(c.hash, samplePassword)
			if err != nil {
				t.Fatalf("Verify returned an unexpected error: %v", err)
			}
			if !v.Valid {
				t.Fatalf("the password was correct; Valid must be true regardless of which policy is checking it")
			}
			if v.NeedsRehash != c.wantNeedsRehash {
				t.Errorf("NeedsRehash = %v, want %v: %s", v.NeedsRehash, c.wantNeedsRehash, c.reason)
			}
		})
	}
}

// --- the decoder accepts exactly what the encoder produces -----------------

func TestSpecVerifyRejectsMalformedHash(t *testing.T) {
	h := crypto.NewHasher(cheapParams(), rand.Reader)
	valid, err := h.Hash(samplePassword)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	fields := strings.Split(valid, "$")
	if len(fields) != 6 {
		t.Fatalf("setup: expected the PHC format's six dollar-delimited segments, got %d in %q — the mutation cases below assume this shape", len(fields), valid)
	}

	mutate := func(f func([]string) []string) string {
		cp := append([]string(nil), fields...)
		cp = f(cp)
		return strings.Join(cp, "$")
	}

	cases := []struct {
		name    string
		encoded string
		reason  string
	}{
		{
			"trailing junk after the version number",
			mutate(func(f []string) []string { f[2] = f[2] + "junk"; return f }),
			`fmt.Sscanf parses "v=19junk" as 19 with no error; a decoder that trusted the parse instead of re-encoding and comparing would wrongly accept this`,
		},
		{
			"trailing junk after the algorithm name",
			mutate(func(f []string) []string { f[1] = f[1] + "junk"; return f }),
			"the same permissive-parse risk applies to every segment, not only the version",
		},
		{
			"wrong algorithm name",
			mutate(func(f []string) []string { f[1] = "argon2i"; return f }),
			"a hash written by a different algorithm must not be accepted as this one's own",
		},
		{
			"wrong version number",
			mutate(func(f []string) []string { f[2] = "v=18"; return f }),
			"the version travels with the hash precisely so a mismatched version is detectable",
		},
		{
			"a missing segment",
			mutate(func(f []string) []string { return append(f[:4], f[5]) }),
			"a hash with a segment removed is not the hash the encoder produced",
		},
		{
			"an empty salt segment",
			mutate(func(f []string) []string { f[4] = ""; return f }),
			"an empty segment is not a valid salt",
		},
		{
			"an extra trailing segment",
			mutate(func(f []string) []string { return append(f, "extra") }),
			"the wrong number of segments is not the format the encoder produced",
		},
		{
			"an unrelated garbage string",
			"not-a-hash-at-all",
			"arbitrary input must not be mistaken for a hash",
		},
		{
			"an empty string",
			"",
			"an empty stored value is not a readable hash",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := h.Verify(c.encoded, samplePassword)
			if err == nil {
				t.Fatalf("Verify accepted a malformed hash (%q): %s", c.encoded, c.reason)
			}
			if !errors.Is(err, crypto.ErrHashUnreadable) {
				t.Errorf("expected ErrHashUnreadable, got %v: %s", err, c.reason)
			}
			if v.Valid {
				t.Errorf("Valid must not be true when the hash could not be read")
			}
		})
	}
}

// --- Hasher.Dummy -----------------------------------------------------

func TestSpecHasherDummyIsStableAcrossCalls(t *testing.T) {
	h := crypto.NewHasher(cheapParams(), rand.Reader)
	a := h.Dummy()
	b := h.Dummy()
	if a != b {
		t.Errorf("Dummy() returned different values on successive calls (%q then %q); the doc states it is computed once at construction, not lazily on each call — a lazily-built dummy makes the first not-found sign-in measurably slower than the rest", a, b)
	}
}

func TestSpecHasherDummyIsAReadableHash(t *testing.T) {
	h := crypto.NewHasher(cheapParams(), rand.Reader)
	dummy := h.Dummy()

	fields := strings.Split(dummy, "$")
	if len(fields) != 6 {
		t.Fatalf("Dummy() = %q does not have the six-segment PHC shape; a caller is told to Verify against it for every not-found account, which only costs what a real Verify costs if it is a well-formed hash", dummy)
	}
	if fields[1] != "argon2id" {
		t.Errorf("Dummy()'s algorithm segment = %q, want %q", fields[1], "argon2id")
	}

	if _, err := h.Verify(dummy, "whatever a caller happened to type"); err != nil {
		t.Errorf("Verify(Dummy(), anything) returned an error (%v); the whole point of Dummy is that it is a real, decodable hash a not-found sign-in can be checked against at full cost", err)
	}
}

// --- HashToken / EqualToken ------------------------------------------

func TestSpecHashTokenIsDeterministic(t *testing.T) {
	// HashToken's signature (string) -> string has no entropy input, so it
	// must be a pure function; a session token's hash is looked up in a
	// database, which only works if hashing the same token twice gives the
	// same result.
	token := "a-sample-session-token-value"
	a := crypto.HashToken(token)
	b := crypto.HashToken(token)
	if a != b {
		t.Errorf("HashToken must be a pure function: hashing the same token twice produced %q and %q", a, b)
	}
}

func TestSpecHashTokenDiffersForDifferentInputs(t *testing.T) {
	tokens := []string{
		"token-one",
		"token-two",
		strings.Repeat("x", 64),
		"",
	}
	seen := map[string]string{}
	for _, tok := range tokens {
		h := crypto.HashToken(tok)
		if prior, ok := seen[h]; ok {
			t.Errorf("HashToken(%q) and HashToken(%q) collided at %q", tok, prior, h)
		}
		seen[h] = tok
	}
}

func TestSpecHashTokenProducesFixedLengthOutput(t *testing.T) {
	inputs := []string{"", "a", strings.Repeat("b", 1000)}
	want := -1
	for _, in := range inputs {
		out := crypto.HashToken(in)
		if out == "" {
			t.Errorf("HashToken(%q) returned an empty string; SHA-256 always produces a fixed-size digest, even for empty input", in)
			continue
		}
		if want == -1 {
			want = len(out)
			continue
		}
		if len(out) != want {
			t.Errorf("HashToken's output length varies with input length: got %d for a %d-byte input, want %d — a fixed-size hash's encoded length must not depend on what was hashed", len(out), len(in), want)
		}
	}
}

func TestSpecEqualTokenAgreesWithHashToken(t *testing.T) {
	cases := []struct {
		name  string
		token string
	}{
		{"an ordinary token", "a-sample-session-token"},
		{"a long token", strings.Repeat("k", 40)},
		{"the empty string as a token", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hash := crypto.HashToken(c.token)
			if !crypto.EqualToken(hash, c.token) {
				t.Errorf("EqualToken(HashToken(t), t) was false for %q; the doc states EqualToken agrees with HashToken", c.token)
			}
		})
	}
}

func TestSpecEqualTokenRejectsAnyOtherToken(t *testing.T) {
	hash := crypto.HashToken("the-real-token")
	if crypto.EqualToken(hash, "not-the-real-token") {
		t.Errorf("EqualToken matched a token against a different token's hash")
	}
}

func TestSpecEqualTokenArgumentOrderIsHashThenToken(t *testing.T) {
	// The signature is EqualToken(hash, token string): the hash first, the
	// candidate token second. Reversing them is a caller bug this function
	// must not silently paper over.
	token := "the-real-token"
	hash := crypto.HashToken(token)

	if !crypto.EqualToken(hash, token) {
		t.Fatalf("setup: EqualToken(hash, token) must be true for a matching pair")
	}
	if crypto.EqualToken(token, hash) {
		t.Errorf("EqualToken(token, hash) with the arguments reversed unexpectedly matched")
	}
}

func TestSpecTokenBytesIsThirtyTwo(t *testing.T) {
	if crypto.TokenBytes != 32 {
		t.Errorf("TokenBytes = %d, want 32 — the doc states a token is 256 random bits", crypto.TokenBytes)
	}
}

// --- Minter -------------------------------------------------------------

func TestSpecMinterNewProducesAUsableTokenPair(t *testing.T) {
	m := crypto.NewMinter(rand.Reader)
	tok, err := m.New()
	if err != nil {
		t.Fatalf("New() returned an unexpected error: %v", err)
	}

	plaintext := tok.Plaintext.Reveal()
	if plaintext == "" {
		t.Fatalf("the minted token's plaintext must not be empty")
	}
	if tok.Hash == "" {
		t.Fatalf("the minted token's hash must not be empty")
	}
	if tok.Hash == plaintext {
		t.Errorf("Token.Hash equals Token.Plaintext.Reveal(); the hash must not be the plaintext — that is the entire point of storing one and returning the other")
	}
	if !crypto.EqualToken(tok.Hash, plaintext) {
		t.Errorf("EqualToken(tok.Hash, tok.Plaintext.Reveal()) is false; New() must return a hash that actually corresponds to the plaintext it hands back")
	}
}

func TestSpecMinterNewProducesADifferentTokenEachCall(t *testing.T) {
	m := crypto.NewMinter(rand.Reader)
	a, err := m.New()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	b, err := m.New()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if a.Plaintext.Reveal() == b.Plaintext.Reveal() {
		t.Errorf("two calls to New() produced the same plaintext; a session token has 256 random bits precisely so this does not happen")
	}
	if a.Hash == b.Hash {
		t.Errorf("two calls to New() produced the same stored hash")
	}
}

func TestSpecMinterNewConsumesExactlyTokenBytesOfEntropy(t *testing.T) {
	// Inference: this assumes Minter.New draws its randomness for a token
	// in exactly TokenBytes worth of reads from the injected entropy
	// source, with nothing else in New() consuming entropy. The spec
	// states "256 random bits" and "TokenBytes = 32" but never states that
	// New() reads exactly that many bytes from the source and no more —
	// that is the natural reading of a token being "256 random bits," not
	// a promise made in so many words.
	counter := &countingReader{r: rand.Reader}
	m := crypto.NewMinter(counter)
	if _, err := m.New(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if counter.n != crypto.TokenBytes {
		t.Errorf("New() consumed %d bytes from the entropy source, want exactly TokenBytes (%d)", counter.n, crypto.TokenBytes)
	}
}

func TestSpecMinterNewFailsWhenEntropyIsUnavailable(t *testing.T) {
	cases := []struct {
		name    string
		entropy io.Reader
	}{
		{"the entropy source errors immediately", failingReader{}},
		{"the entropy source is empty from the first read", newShortReader(0)},
		{"the entropy source provides fewer than TokenBytes before ending", newShortReader(crypto.TokenBytes / 2)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := crypto.NewMinter(c.entropy)
			_, err := m.New()
			if err == nil {
				t.Errorf("New() succeeded with an entropy source that could not supply TokenBytes (%d) bytes; minting must fail rather than return a weak or short token", crypto.TokenBytes)
			}
		})
	}
}

// --- Token.Plaintext redaction ------------------------------------------

func TestSpecTokenPlaintextRedactsThroughFmt(t *testing.T) {
	m := crypto.NewMinter(rand.Reader)
	tok, err := m.New()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	real := tok.Plaintext.Reveal()

	forms := []struct {
		verb     string
		rendered string
	}{
		{"%s", fmt.Sprintf("%s", tok.Plaintext)},
		{"%v", fmt.Sprintf("%v", tok.Plaintext)},
	}
	for _, f := range forms {
		if strings.Contains(f.rendered, real) {
			t.Errorf("formatting Token.Plaintext with %s leaked the real value (%q appeared in %q) — a session token in a log line is the same breach as one in a table", f.verb, real, f.rendered)
		}
	}

	if tok.Plaintext.String() == real {
		t.Errorf("Plaintext.String() equals Plaintext.Reveal(); String() is documented as the redacted form and Reveal() as the only way out, so for a non-empty token they must differ")
	}
}

func TestSpecTokenPlaintextRedactsThroughJSON(t *testing.T) {
	m := crypto.NewMinter(rand.Reader)
	tok, err := m.New()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	real := tok.Plaintext.Reveal()

	out, err := json.Marshal(tok.Plaintext)
	if err != nil {
		t.Fatalf("json.Marshal(Token.Plaintext) returned an unexpected error: %v", err)
	}
	if strings.Contains(string(out), real) {
		t.Errorf("json.Marshal(Token.Plaintext) leaked the real value: %s", out)
	}
}
