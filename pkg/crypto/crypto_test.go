package crypto_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

// cheap keeps the suite fast. Every property under test is about encoding,
// parsing and comparison, none of which cares how expensive the KDF was —
// except the one test that uses Default deliberately.
var cheap = crypto.Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func hasher(t *testing.T) *crypto.Hasher {
	t.Helper()
	return crypto.NewHasher(cheap, rand.Reader)
}

func TestAPasswordVerifiesAgainstItsOwnHashAndNothingElse(t *testing.T) {
	h := hasher(t)
	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}

	v, err := h.Verify(encoded, "correct horse battery staple")
	if err != nil || !v.Valid {
		t.Fatalf("the right password did not verify: %+v %v", v, err)
	}
	if v.NeedsRehash {
		t.Error("a hash made at current policy wants rehashing")
	}

	// A wrong password is NOT an error. It is a successful comparison with a
	// negative answer, and returning an error here would make a caller unable
	// to tell "wrong password" from "the database is down".
	v, err = h.Verify(encoded, "correct horse battery stapl")
	if err != nil {
		t.Errorf("a wrong password came back as an error: %v", err)
	}
	if v.Valid {
		t.Error("a wrong password verified")
	}
}

func TestTheSamePasswordHashesDifferentlyEveryTime(t *testing.T) {
	h := hasher(t)
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		encoded, err := h.Hash("same")
		if err != nil {
			t.Fatal(err)
		}
		if seen[encoded] {
			t.Fatal("two hashes of the same password are identical — the salt is not random")
		}
		seen[encoded] = true
		if v, _ := h.Verify(encoded, "same"); !v.Valid {
			t.Fatal("a freshly salted hash did not verify")
		}
	}
}

func TestAnEmptyPasswordIsRefusedRatherThanHashed(t *testing.T) {
	h := hasher(t)
	if _, err := h.Hash(""); !errors.Is(err, crypto.ErrPasswordEmpty) {
		t.Fatalf("hashing an empty password gave %v", err)
	}
}

// The property the PHC encoding exists for: raising the cost must not
// invalidate a single stored hash, and the caller must be told to rehash.
func TestAHashMadeUnderWeakerParametersStillVerifiesAndAsksToBeRehashed(t *testing.T) {
	weak := crypto.NewHasher(cheap, rand.Reader)
	encoded, err := weak.Hash("secret")
	if err != nil {
		t.Fatal(err)
	}

	stronger := cheap
	stronger.Iterations = 2
	strong := crypto.NewHasher(stronger, rand.Reader)

	v, err := strong.Verify(encoded, "secret")
	if err != nil {
		t.Fatalf("a hash from the old policy would not verify: %v", err)
	}
	if !v.Valid {
		t.Fatal("a hash from the old policy verified as wrong")
	}
	if !v.NeedsRehash {
		t.Error("a hash below current policy did not ask to be rehashed")
	}

	rehashed, err := strong.Hash("secret")
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := strong.Verify(rehashed, "secret"); !v.Valid || v.NeedsRehash {
		t.Errorf("the rehashed value is not at policy: %+v", v)
	}
}

func TestADecoderAcceptsExactlyWhatTheEncoderProduces(t *testing.T) {
	h := hasher(t)
	good, err := h.Hash("x")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(good, "$")

	corrupt := map[string]string{
		"empty":                     "",
		"not PHC at all":            "not-a-hash",
		"too few segments":          "$argon2id$v=19$m=64,t=1,p=1$" + parts[4],
		"too many segments":         good + "$extra",
		"no leading dollar":         strings.TrimPrefix(good, "$"),
		"another algorithm":         strings.Replace(good, "argon2id", "argon2i", 1),
		"bcrypt":                    "$2a$10$abcdefghijklmnopqrstuv",
		"trailing junk on version":  strings.Replace(good, "v=19$", "v=19junk$", 1),
		"trailing junk on params":   strings.Replace(good, "p=1$", "p=1junk$", 1),
		"an unreadable version":     strings.Replace(good, "v=19", "v=xx", 1),
		"an unknown argon2 version": strings.Replace(good, "v=19", "v=18", 1),
		"unreadable parameters":     strings.Replace(good, "m=64,t=1,p=1", "m=a,t=b,p=c", 1),
		"a salt that is not base64": strings.Replace(good, parts[4], "!!!!", 1),
		"a digest that is not b64":  strings.Replace(good, parts[5], "!!!!", 1),
		"an empty salt":             strings.Replace(good, parts[4], "", 1),
		"an empty digest":           strings.Replace(good, parts[5], "", 1),
	}
	for name, encoded := range corrupt {
		t.Run(name, func(t *testing.T) {
			v, err := h.Verify(encoded, "x")
			if err == nil {
				t.Fatalf("accepted %q as a hash, and reported valid=%v", encoded, v.Valid)
			}
			if !errors.Is(err, crypto.ErrHashUnreadable) {
				t.Errorf("refused it as %v, not ErrHashUnreadable", err)
			}
			if v.Valid {
				t.Error("an unreadable hash reported a valid password")
			}
		})
	}
}

// The enumeration defence. Dummy has to be a REAL hash at current parameters,
// or verifying against it is cheaper than verifying against a stored one and the
// timing difference is the leak it was built to close.
func TestDummyIsARealHashAtCurrentParametersAndIsStable(t *testing.T) {
	h := hasher(t)
	d := h.Dummy()
	if d == "" {
		t.Fatal("there is no dummy")
	}
	if d != h.Dummy() {
		t.Error("the dummy changes between calls — it is being computed lazily")
	}
	// Verifying an arbitrary password against it must do the full work and come
	// back false, exactly like a wrong password against a real hash.
	v, err := h.Verify(d, "whatever the caller typed")
	if err != nil {
		t.Fatalf("the dummy is not a readable hash: %v", err)
	}
	if v.Valid {
		t.Error("an arbitrary password verified against the dummy")
	}
	if !strings.HasPrefix(d, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Errorf("the dummy was not made at the hasher's own parameters: %q", d)
	}
}

func TestDefaultIsRFC9106sSecondRecommendationAndRoundTrips(t *testing.T) {
	if crypto.Default.Memory != 64*1024 || crypto.Default.Iterations != 3 ||
		crypto.Default.Parallelism != 4 || crypto.Default.SaltLength != 16 ||
		crypto.Default.KeyLength != 32 {
		t.Fatalf("Default drifted from the cited parameters: %+v", crypto.Default)
	}
	h := crypto.NewHasher(crypto.Default, rand.Reader)
	encoded, err := h.Hash("expensive")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("encoded as %q", encoded)
	}
	if v, _ := h.Verify(encoded, "expensive"); !v.Valid || v.NeedsRehash {
		t.Errorf("Default did not round-trip: %+v", v)
	}
}

// exhausting reports EOF after n successful reads. NewHasher computes the dummy
// eagerly, so the source has to survive construction and fail afterwards — which
// is exactly the shape of a real entropy failure at runtime.
type exhausting struct {
	left int
}

func (e *exhausting) Read(p []byte) (int, error) {
	if e.left <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	e.left--
	return rand.Read(p)
}

func TestAFailingEntropySourceIsAnErrorAndNotASilentWeakSalt(t *testing.T) {
	// one read for the dummy at construction, nothing after it
	h := crypto.NewHasher(cheap, &exhausting{left: 1})
	_, err := h.Hash("password")
	if err == nil {
		t.Fatal("a hash was produced with no entropy — the salt would be sixteen zero bytes")
	}
	if !errors.IsKind(err, errors.Internal) {
		t.Errorf("the failure came back as %v, want Internal", err)
	}

	m := crypto.NewMinter(&exhausting{left: 0})
	if _, err := m.New(); err == nil {
		t.Fatal("a token was minted with no entropy")
	}
}

// A source that fails during construction is a boot failure, not a runtime one.
func TestAHasherRefusesToExistWithoutEnoughEntropyForItsDummy(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewHasher returned a hasher whose dummy could not be computed")
		}
	}()
	crypto.NewHasher(cheap, bytes.NewReader(nil))
}

func TestATokenIsMintedWithItsHashAndTheHalvesAgree(t *testing.T) {
	m := crypto.NewMinter(rand.Reader)
	tok, err := m.New()
	if err != nil {
		t.Fatal(err)
	}
	plaintext := tok.Plaintext.Reveal()
	if len(plaintext) < 40 {
		t.Errorf("a %d-character token is not 256 bits of base64", len(plaintext))
	}
	if tok.Hash != crypto.HashToken(plaintext) {
		t.Error("the minted hash is not the hash of the minted plaintext")
	}
	if !crypto.EqualToken(tok.Hash, plaintext) {
		t.Error("EqualToken refused the pair it was handed")
	}
	if crypto.EqualToken(tok.Hash, plaintext+"x") {
		t.Error("EqualToken accepted a token that was not the one")
	}

	second, err := m.New()
	if err != nil {
		t.Fatal(err)
	}
	if second.Plaintext.Reveal() == plaintext || second.Hash == tok.Hash {
		t.Error("two mints produced the same token")
	}
}

// A session token in a log line is the same breach as one in a table. The type
// is what makes that unwriteable rather than merely discouraged.
func TestAMintedTokenCannotBeLoggedOrSerialised(t *testing.T) {
	m := crypto.NewMinter(rand.Reader)
	tok, err := m.New()
	if err != nil {
		t.Fatal(err)
	}
	plaintext := tok.Plaintext.Reveal()

	for name, rendered := range map[string]string{
		"%v":     fmt.Sprintf("%v", tok.Plaintext),
		"%s":     fmt.Sprintf("%s", tok.Plaintext),
		"%q":     fmt.Sprintf("%q", tok.Plaintext),
		"%#v":    fmt.Sprintf("%#v", tok.Plaintext),
		"struct": fmt.Sprintf("%v", tok),
	} {
		if strings.Contains(rendered, plaintext) {
			t.Errorf("%s rendered the token in full: %s", name, rendered)
		}
		if name != "struct" && !strings.Contains(rendered, secret.Redacted) {
			t.Errorf("%s rendered %q, expected the redaction marker", name, rendered)
		}
	}

	b, err := json.Marshal(tok)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), plaintext) {
		t.Errorf("encoding/json wrote the token: %s", b)
	}
	// The hash is NOT a secret and must still be readable — it is what a
	// repository stores, and redacting it would break every caller.
	if !strings.Contains(string(b), tok.Hash) {
		t.Errorf("the hash was redacted too: %s", b)
	}
}

func TestConstructionRefusesWhatCannotWork(t *testing.T) {
	bad := map[string]func(){
		"a nil entropy source": func() { crypto.NewHasher(cheap, nil) },
		"a nil minter source":  func() { crypto.NewMinter(nil) },
		"zero iterations": func() {
			crypto.NewHasher(crypto.Params{Iterations: 0, Parallelism: 1, Memory: 64, SaltLength: 16, KeyLength: 32}, rand.Reader)
		},
		"zero parallelism": func() {
			crypto.NewHasher(crypto.Params{Iterations: 1, Parallelism: 0, Memory: 64, SaltLength: 16, KeyLength: 32}, rand.Reader)
		},
		"too little memory": func() {
			crypto.NewHasher(crypto.Params{Iterations: 1, Parallelism: 4, Memory: 8, SaltLength: 16, KeyLength: 32}, rand.Reader)
		},
		"a short salt": func() {
			crypto.NewHasher(crypto.Params{Iterations: 1, Parallelism: 1, Memory: 64, SaltLength: 4, KeyLength: 32}, rand.Reader)
		},
		"a short key": func() {
			crypto.NewHasher(crypto.Params{Iterations: 1, Parallelism: 1, Memory: 64, SaltLength: 16, KeyLength: 8}, rand.Reader)
		},
	}
	for name, call := range bad {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("construction succeeded")
				}
			}()
			call()
		})
	}
}

// The case doc.go had no answer for until 2026-09-06, and that neither suite
// covered: Params has no total order, so a hash with more memory and fewer
// iterations is neither above nor below policy as a whole. The rule is per
// dimension — ANY dimension below policy asks for a rehash.
func TestRehashIsDecidedPerDimensionBecauseParamsHaveNoTotalOrder(t *testing.T) {
	policy := crypto.Params{Memory: 128, Iterations: 2, Parallelism: 2, SaltLength: 16, KeyLength: 32}
	h := crypto.NewHasher(policy, rand.Reader)

	weaker := func(f func(*crypto.Params)) *crypto.Hasher {
		p := policy
		f(&p)
		return crypto.NewHasher(p, rand.Reader)
	}

	for name, tc := range map[string]struct {
		stored *crypto.Hasher
		want   bool
	}{
		"less memory":      {weaker(func(p *crypto.Params) { p.Memory = 64 }), true},
		"fewer iterations": {weaker(func(p *crypto.Params) { p.Iterations = 1 }), true},
		"less parallelism": {weaker(func(p *crypto.Params) { p.Parallelism = 1 }), true},
		"a shorter salt":   {weaker(func(p *crypto.Params) { p.SaltLength = 8 }), true},
		"a shorter key":    {weaker(func(p *crypto.Params) { p.KeyLength = 16 }), true},

		"more memory":     {weaker(func(p *crypto.Params) { p.Memory = 256 }), false},
		"more iterations": {weaker(func(p *crypto.Params) { p.Iterations = 4 }), false},
		"a longer salt":   {weaker(func(p *crypto.Params) { p.SaltLength = 32 }), false},
		"a longer key":    {weaker(func(p *crypto.Params) { p.KeyLength = 64 }), false},

		// Neither above nor below as a whole. One dimension under policy is
		// enough, because the alternative is a cost model argon2 does not supply.
		"more memory, fewer iterations": {
			weaker(func(p *crypto.Params) { p.Memory = 256; p.Iterations = 1 }), true},
		"more iterations, less memory": {
			weaker(func(p *crypto.Params) { p.Iterations = 4; p.Memory = 64 }), true},
	} {
		t.Run(name, func(t *testing.T) {
			stored, err := tc.stored.Hash("secret")
			if err != nil {
				t.Fatal(err)
			}
			v, err := h.Verify(stored, "secret")
			if err != nil {
				t.Fatalf("a hash at other parameters would not verify: %v", err)
			}
			if !v.Valid {
				t.Fatal("it verified as wrong")
			}
			if v.NeedsRehash != tc.want {
				t.Errorf("NeedsRehash = %v, want %v — rehashing a stronger hash weakens it", v.NeedsRehash, tc.want)
			}
		})
	}
}

func TestHashTokenIsLowercaseHexAndThatIsPartOfTheContract(t *testing.T) {
	h := crypto.HashToken("a-token")
	if len(h) != 64 {
		t.Errorf("a sha256 in hex is 64 characters, got %d: %q", len(h), h)
	}
	for _, c := range h {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			t.Fatalf("%q is not lowercase hex: %q", c, h)
		}
	}
	// A repository stores this in a text column and compares against it, so the
	// encoding changing invalidates every stored row.
	if crypto.HashToken("a-token") != h {
		t.Error("HashToken is not deterministic")
	}
}
