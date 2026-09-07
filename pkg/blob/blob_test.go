package blob_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/errors"
)

func store(t *testing.T) *blob.Store {
	t.Helper()
	s, err := blob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTheNameIsTheHashOfTheContents(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	body := "server: nginx/1.25.3\n"

	got, err := s.Put(ctx, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(body))
	if got.Ref.Hex != hex.EncodeToString(want[:]) {
		t.Errorf("ref %s is not the sha256 of the bytes", got.Ref)
	}
	if got.Ref.Algo != blob.Algo || got.Size != int64(len(body)) {
		t.Errorf("%+v", got)
	}

	r, err := s.Open(ctx, got.Ref)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	back, _ := io.ReadAll(r)
	if string(back) != body {
		t.Errorf("read back %q", back)
	}
}

// The dedup the corpus needs, with no dedup table to keep consistent. A retried
// invocation cannot produce a second artifact.
func TestStoringTheSameBytesTwiceIsOneFileAndOneAnswer(t *testing.T) {
	root := t.TempDir()
	s, err := blob.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	first, err := s.Put(ctx, strings.NewReader("identical"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Put(ctx, strings.NewReader("identical"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Ref != second.Ref {
		t.Fatalf("%s vs %s", first.Ref, second.Ref)
	}

	var files int
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.Contains(p, string(os.PathSeparator)+"tmp"+string(os.PathSeparator)) {
			files++
		}
		return nil
	})
	if files != 1 {
		t.Errorf("%d files on disk for one blob stored twice", files)
	}
}

func TestDifferentBytesAreDifferentBlobs(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	a, _ := s.Put(ctx, strings.NewReader("a"))
	b, _ := s.Put(ctx, strings.NewReader("b"))
	if a.Ref == b.Ref {
		t.Fatal("two different bodies share a ref")
	}
}

// Two bytes of the hash, twice: 65,536 buckets, so a quarter of a million
// artifacts never land in one directory.
func TestBlobsFanOutRatherThanFillingOneDirectory(t *testing.T) {
	root := t.TempDir()
	s, _ := blob.New(root)
	info, err := s.Put(context.Background(), strings.NewReader("fanout"))
	if err != nil {
		t.Fatal(err)
	}
	h := info.Ref.Hex
	want := filepath.Join(root, blob.Algo, h[0:2], h[2:4], h)
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected the blob at %s: %v", want, err)
	}
}

// A record citing bytes that no longer hash to their name is a record that
// stopped being true, and only reading them says so.
func TestVerifyCatchesBytesThatWereChangedUnderneath(t *testing.T) {
	root := t.TempDir()
	s, _ := blob.New(root)
	ctx := context.Background()

	info, err := s.Put(ctx, strings.NewReader("the original bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(ctx, info.Ref); err != nil {
		t.Fatalf("a freshly written blob did not verify: %v", err)
	}

	h := info.Ref.Hex
	if err := os.WriteFile(filepath.Join(root, blob.Algo, h[0:2], h[2:4], h),
		[]byte("tampered"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(ctx, info.Ref); !errors.Is(err, blob.ErrCorrupted) {
		t.Errorf("gave %v, want ErrCorrupted", err)
	}
}

func TestAMissingBlobIsNotFoundAndNotAFailure(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	ref, err := blob.ParseRef("sha256:" + strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(ctx, ref); !errors.Is(err, blob.ErrNotFound) {
		t.Errorf("Open gave %v", err)
	}
	if _, err := s.Stat(ctx, ref); !errors.Is(err, blob.ErrNotFound) {
		t.Errorf("Stat gave %v", err)
	}
}

func TestARefIsParsedStrictlyOrRefused(t *testing.T) {
	good := "sha256:" + strings.Repeat("0f", 32)
	ref, err := blob.ParseRef(good)
	if err != nil || ref.String() != good {
		t.Fatalf("%v %v", ref, err)
	}
	for _, bad := range []string{
		"",
		"deadbeef",
		"md5:" + strings.Repeat("0f", 32),
		"sha256:short",
		"sha256:" + strings.Repeat("zz", 32),
		"sha256:" + strings.Repeat("0F", 32), // upper case is a second spelling
	} {
		if _, err := blob.ParseRef(bad); !errors.Is(err, blob.ErrBadRef) {
			t.Errorf("ParseRef(%q) gave %v, want ErrBadRef", bad, err)
		}
	}
}

// A crash mid-write must leave a temp file, never a truncated blob under a name
// that promises complete contents.
func TestAFailedWriteLeavesNoBlobBehind(t *testing.T) {
	root := t.TempDir()
	s, _ := blob.New(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.Put(ctx, strings.NewReader("never finished")); err == nil {
		t.Fatal("a cancelled write succeeded")
	}
	var placed int
	_ = filepath.WalkDir(filepath.Join(root, blob.Algo), func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			placed++
		}
		return nil
	})
	if placed != 0 {
		t.Errorf("%d blobs from a cancelled write", placed)
	}
}
