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
