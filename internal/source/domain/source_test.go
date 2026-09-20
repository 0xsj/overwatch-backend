package domain_test

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestSourceIntakeValidation(t *testing.T) {
	body := "retained text"
	empty := ""
	invalid := string([]byte{0xff})
	cases := []struct {
		name  string
		draft domain.Draft
		valid bool
	}{
		{"paste", domain.Draft{Title: "Report", Origin: "paste", Content: &body}, true},
		{"import", domain.Draft{Title: "Report", Origin: "import", Filename: "report.txt", Content: &body}, true},
		{"reference", domain.Draft{Title: "Report", Origin: "reference", URL: "https://example.test/report"}, true},
		{"reference needs URL", domain.Draft{Title: "Report", Origin: "reference"}, false},
		{"reference cannot carry initial content", domain.Draft{Title: "Report", Origin: "reference", URL: "https://example.test", Content: &body}, false},
		{"import needs filename", domain.Draft{Title: "Report", Origin: "import", Content: &body}, false},
		{"paste needs content", domain.Draft{Title: "Report", Origin: "paste"}, false},
		{"empty capture", domain.Draft{Title: "Report", Origin: "paste", Content: &empty}, false},
		{"invalid UTF-8", domain.Draft{Title: "Report", Origin: "paste", Content: &invalid}, false},
		{"credentials", domain.Draft{Title: "Report", Origin: "reference", URL: "https://user:password@example.test"}, false},
		{"file URL", domain.Draft{Title: "Report", Origin: "reference", URL: "file:///etc/passwd"}, false},
		{"relative URL", domain.Draft{Title: "Report", Origin: "reference", URL: "//example.test"}, false},
	}
	at := time.Now()
	ids := id.NewSequence(at)
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := domain.New(ids.NewID(), ids.NewID(), ids.NewID(), test.draft, at)
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
	published := time.Date(2026, 9, 18, 10, 30, 0, 0, time.FixedZone("source", 2*60*60))
	source, err := domain.New(ids.NewID(), ids.NewID(), ids.NewID(), domain.Draft{Title: "Published notice", Origin: "reference", URL: "https://example.test/published", PublishedAt: &published}, at)
	if err != nil || source.PublishedAt == nil || !source.PublishedAt.Equal(published.UTC()) {
		t.Fatalf("publication time was not preserved as UTC: %+v err=%v", source, err)
	}
	zero := time.Time{}
	if _, err := domain.New(ids.NewID(), ids.NewID(), ids.NewID(), domain.Draft{Title: "Invalid publication", Origin: "reference", URL: "https://example.test/invalid", PublishedAt: &zero}, at); err == nil {
		t.Fatal("zero publication time accepted")
	}
}

func TestCaptureLimitIsUTF8BytesAndJSONIsValidatedWithoutReformatting(t *testing.T) {
	for _, body := range []string{strings.Repeat("x", domain.MaxCaptureBytes), strings.Repeat("é", domain.MaxCaptureBytes/2), "\n\x00text\r\n"} {
		if _, err := domain.ValidateContent("text/plain", body); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{strings.Repeat("x", domain.MaxCaptureBytes+1), strings.Repeat("é", domain.MaxCaptureBytes/2+1)} {
		if _, err := domain.ValidateContent("text/plain", body); err == nil {
			t.Fatal("oversized capture accepted")
		}
	}
	if _, err := domain.ValidateContent("application/json", " {\n \"ok\": true\n }\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.ValidateContent("application/json", "{broken}"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := domain.ValidateContent("text/html", "<script>anything</script>"); err != nil {
		t.Fatal("HTML media type rejected: ", err)
	}
}

func TestBinaryCaptureValidationRequiresDeclaredSignatures(t *testing.T) {
	if err := domain.ValidateBinary("application/pdf", []byte("%PDF-1.7\nbody")); err != nil {
		t.Fatal(err)
	}
	if err := domain.ValidateBinary("image/png", []byte("not a png")); err == nil {
		t.Fatal("invalid PNG accepted")
	}
	if err := domain.ValidateBinary("application/pdf", []byte("not a PDF")); err == nil {
		t.Fatal("invalid PDF accepted")
	}
}

func TestNewCaptureAcceptsValidatedBinaryMedia(t *testing.T) {
	content := []byte("%PDF-1.7\nbytes")
	sum := sha256.Sum256(content)
	ids := id.NewSequence(time.Unix(1, 0))
	capture, err := domain.NewCapture(
		ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(),
		1,
		"application/pdf",
		blob.Info{Ref: blob.Ref{Algo: "sha256", Hex: hex.EncodeToString(sum[:])}, Size: int64(len(content))},
		time.Unix(5, 0),
	)
	if err != nil || capture.MediaType != "application/pdf" || capture.Bytes != int64(len(content)) {
		t.Fatalf("binary capture rejected: %+v err=%v", capture, err)
	}
}

func TestSourceRetentionIsExplicitAndClearable(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	source, err := domain.New(id.ID{1}, id.ID{2}, id.ID{3}, domain.Draft{Title: "Notice", Origin: "reference", URL: "https://example.test/notice"}, at)
	if err != nil {
		t.Fatal(err)
	}
	until := at.Add(30 * 24 * time.Hour)
	updated, err := source.SetRetention(id.ID{4}, &until, at)
	if err != nil || updated.RetentionUntil == nil || !updated.RetentionUntil.Equal(until) || updated.RetentionUpdatedBy != (id.ID{4}) {
		t.Fatalf("retention update: %+v, error=%v", updated, err)
	}
	cleared, err := updated.SetRetention(id.ID{5}, nil, at.Add(time.Hour))
	if err != nil || cleared.RetentionUntil != nil || cleared.RetentionUpdatedBy != (id.ID{5}) {
		t.Fatalf("retention clear: %+v, error=%v", cleared, err)
	}
}

func TestSourcePublicationIsExplicitAndClearable(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	source, err := domain.New(id.ID{1}, id.ID{2}, id.ID{3}, domain.Draft{Title: "Notice", Origin: "reference", URL: "https://example.test/notice"}, at)
	if err != nil {
		t.Fatal(err)
	}
	published := time.Date(2026, 9, 16, 14, 0, 0, 0, time.FixedZone("source", 2*60*60))
	updated, err := source.SetPublication(id.ID{4}, &published, at)
	if err != nil || updated.PublishedAt == nil || !updated.PublishedAt.Equal(published.UTC()) {
		t.Fatalf("publication update: %+v, error=%v", updated, err)
	}
	cleared, err := updated.SetPublication(id.ID{5}, nil, at.Add(time.Hour))
	if err != nil || cleared.PublishedAt != nil {
		t.Fatalf("publication clear: %+v, error=%v", cleared, err)
	}
	zero := time.Time{}
	if _, err := source.SetPublication(id.ID{6}, &zero, at); err == nil {
		t.Fatal("zero publication time accepted")
	}
}

func TestSourceDuplicatePolicyIsBounded(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	source, err := domain.New(id.ID{1}, id.ID{2}, id.ID{3}, domain.Draft{Title: "Notice", Origin: "reference", URL: "https://example.test/notice"}, at)
	if err != nil || source.DuplicatePolicy != domain.DuplicatePolicyWarn {
		t.Fatalf("default duplicate policy: %+v, error=%v", source, err)
	}
	updated, err := source.SetDuplicatePolicy(id.ID{4}, domain.DuplicatePolicyBlock, at)
	if err != nil || updated.DuplicatePolicy != domain.DuplicatePolicyBlock {
		t.Fatalf("duplicate policy update: %+v, error=%v", updated, err)
	}
	if _, err := source.SetDuplicatePolicy(id.ID{4}, "merge", at); err == nil {
		t.Fatal("invalid duplicate policy accepted")
	}
}

func TestIntakeCandidateRequiresExplicitReview(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	candidate, err := domain.NewIntakeCandidate(id.ID{1}, id.ID{2}, id.ID{3}, "Notice", "https://example.test/notice", "Found in a monitored feed.", at)
	if err != nil || candidate.Status != domain.IntakePending {
		t.Fatalf("candidate: %+v, error=%v", candidate, err)
	}
	approved, err := candidate.Review(id.ID{4}, domain.IntakeApproved, "Relevant to the investigation.", id.ID{5}, at.Add(time.Hour))
	if err != nil || approved.Status != domain.IntakeApproved || approved.SourceID != (id.ID{5}) {
		t.Fatalf("approval: %+v, error=%v", approved, err)
	}
	if _, err := approved.Review(id.ID{4}, domain.IntakeRejected, "No longer needed.", id.ID{}, at.Add(2*time.Hour)); err == nil {
		t.Fatal("reviewed candidate was reopened")
	}
	if _, err := domain.NewIntakeCandidate(id.ID{6}, id.ID{2}, id.ID{3}, "Bad", "ftp://example.test/notice", "note", at); err == nil {
		t.Fatal("non-web intake URL accepted")
	}
	imported, err := domain.NewImportIntakeCandidate(id.ID{7}, id.ID{2}, id.ID{3}, "Notice PDF", "notice.pdf", "application/pdf", []byte("%PDF-1.7\nnotice"), "Imported for review.", at)
	if err != nil || imported.Origin != domain.IntakeImport || imported.Filename != "notice.pdf" || len(imported.ContentBytes) == 0 {
		t.Fatalf("import candidate: %+v, error=%v", imported, err)
	}
}

func TestSourceWatchSchedulesAndRecordsQualifiedRuns(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	watch, err := domain.NewWatch(id.ID{1}, id.ID{2}, id.ID{3}, true, domain.MinWatchIntervalSeconds, at)
	if err != nil || !watch.Enabled || watch.NextRunAt == nil || watch.LastStatus != domain.WatchStatusNever {
		t.Fatalf("new watch: %+v, error=%v", watch, err)
	}
	unchanged, err := watch.RecordRun(id.ID{4}, domain.WatchStatusUnchanged, id.ID{}, "", at.Add(time.Hour))
	if err != nil || unchanged.LastStatus != domain.WatchStatusUnchanged || unchanged.LastCaptureID != (id.ID{}) || unchanged.NextRunAt == nil {
		t.Fatalf("unchanged run: %+v, error=%v", unchanged, err)
	}
	changed, err := unchanged.RecordRun(id.ID{4}, domain.WatchStatusChanged, id.ID{5}, "", at.Add(2*time.Hour))
	if err != nil || changed.LastStatus != domain.WatchStatusChanged || changed.LastCaptureID != (id.ID{5}) {
		t.Fatalf("changed run: %+v, error=%v", changed, err)
	}
	if _, err := changed.RecordRun(id.ID{4}, domain.WatchStatusChanged, id.ID{}, "", at); err == nil {
		t.Fatal("changed run without a capture was accepted")
	}
	if _, err := watch.Configure(id.ID{4}, false, domain.MaxWatchIntervalSeconds+1, at); err == nil {
		t.Fatal("out-of-range watch interval accepted")
	}
	claimed, err := watch.Claim("worker-a", at.Add(time.Hour), at.Add(time.Hour+2*time.Minute))
	if err != nil || claimed.LeaseOwner != "worker-a" || claimed.LeaseUntil == nil {
		t.Fatalf("due watch was not claimed: %+v, error=%v", claimed, err)
	}
	if _, err := claimed.Claim("worker-b", at.Add(time.Hour+time.Minute), at.Add(time.Hour+3*time.Minute)); err != domain.ErrWatchLeased {
		t.Fatalf("active lease was not protected: %v", err)
	}
	released, err := claimed.RecordClaimedRun(id.ID{4}, domain.WatchStatusUnchanged, id.ID{}, "", "worker-a", at.Add(time.Hour+time.Minute))
	if err != nil || released.LeaseOwner != "" || released.LeaseUntil != nil {
		t.Fatalf("claimed run did not release lease: %+v, error=%v", released, err)
	}
}
