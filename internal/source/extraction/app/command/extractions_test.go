package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/source/extraction/domain"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestPDFTextExtractsTextFromAStoredDocument(t *testing.T) {
	got, err := pdfText(onePagePDF("Hello from the retained PDF."))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Hello from the retained PDF.") {
		t.Fatalf("extracted text=%q", got)
	}
}

func TestPDFTextRejectsMalformedBytesWithoutPanicking(t *testing.T) {
	if _, err := pdfText([]byte("%PDF-1.7\nnot a complete document")); err == nil {
		t.Fatal("malformed PDF was accepted")
	}
}

type extractionRepo struct{ rows []domain.Extraction }

func (r *extractionRepo) Create(_ context.Context, in domain.Extraction) error {
	r.rows = append(r.rows, in)
	return nil
}

type extractionBlobs struct{ bodies [][]byte }

func (b *extractionBlobs) Put(_ context.Context, reader io.Reader) (blob.Info, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return blob.Info{}, err
	}
	b.bodies = append(b.bodies, body)
	sum := sha256.Sum256(body)
	return blob.Info{Ref: blob.Ref{Algo: blob.Algo, Hex: hex.EncodeToString(sum[:])}, Size: int64(len(body))}, nil
}

type extractionCapture struct{ retained RetainedCapture }

func (c extractionCapture) Retained(context.Context, id.ID, id.ID, id.ID) (RetainedCapture, error) {
	return c.retained, nil
}

type extractionTransaction struct{}

func (extractionTransaction) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type extractionPublisher struct{}

func (extractionPublisher) Publish(context.Context, ...events.Event) error { return nil }

type extractionOCR func(context.Context, RetainedCapture) (string, error)

func (f extractionOCR) Extract(ctx context.Context, retained RetainedCapture) (string, error) {
	return f(ctx, retained)
}

func TestImageOCRWithoutAConfiguredProviderRecordsAnUnsupportedAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, source, capture, author := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &extractionRepo{}
	blobs := &extractionBlobs{}
	service := NewExtractions(repo, blobs, extractionCapture{retained: RetainedCapture{
		WorkspaceID: workspace, SourceID: source, CaptureID: capture, MediaType: "image/png", Bytes: []byte("\x89PNG\r\n\x1a\nimage"),
	}}, extractionTransaction{}, extractionPublisher{}, ids, fixedExtractionClock{at})

	got, err := service.Extract(context.Background(), workspace, source, capture, author)
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != domain.MethodOCRV1 || got.Status != domain.Unsupported || got.Message != ErrOCRUnavailable.Error() || got.OutputHash != "" || got.OutputBytes != 0 {
		t.Fatalf("unexpected OCR attempt: %+v", got)
	}
	if len(blobs.bodies) != 0 || len(repo.rows) != 1 {
		t.Fatalf("unsupported OCR created output: blobs=%d rows=%d", len(blobs.bodies), len(repo.rows))
	}
}

func TestConfiguredImageOCRStoresTextAsTheDerivedArtifact(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, source, capture, author := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &extractionRepo{}
	blobs := &extractionBlobs{}
	var seen RetainedCapture
	service := NewExtractionsWithOCR(repo, blobs, extractionCapture{retained: RetainedCapture{
		WorkspaceID: workspace, SourceID: source, CaptureID: capture, MediaType: "image/jpeg", Bytes: []byte{0xff, 0xd8, 0xff, 0x00},
	}}, extractionTransaction{}, extractionPublisher{}, ids, fixedExtractionClock{at}, extractionOCR(func(_ context.Context, retained RetainedCapture) (string, error) {
		seen = retained
		return "OCR found East Quay.", nil
	}))

	got, err := service.Extract(context.Background(), workspace, source, capture, author)
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != domain.MethodOCRV1 || got.Status != domain.Succeeded || got.OutputBytes != int64(len("OCR found East Quay.")) || got.OutputHash == "" {
		t.Fatalf("unexpected configured OCR extraction: %+v", got)
	}
	if seen.WorkspaceID != workspace || seen.SourceID != source || seen.CaptureID != capture || string(seen.Bytes) != string([]byte{0xff, 0xd8, 0xff, 0x00}) {
		t.Fatalf("OCR provider did not receive the exact retained capture: %+v", seen)
	}
	if len(blobs.bodies) != 1 || string(blobs.bodies[0]) != "OCR found East Quay." {
		t.Fatalf("OCR output was not retained: %q", blobs.bodies)
	}
}

func TestProcessImageOCRPassesTheExactCaptureToAConfiguredExecutable(t *testing.T) {
	provider := NewProcessImageOCR(ProcessImageOCRConfig{
		Binary: "/bin/sh",
		Args:   []string{"-c", `test "$(wc -c < "$1")" -eq 4 && printf 'OCR found East Quay.'`, "ocr-test", OCRInputPlaceholder},
	})
	got, err := provider.Extract(context.Background(), RetainedCapture{MediaType: "image/png", Bytes: []byte{0x89, 'P', 'N', 'G'}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "OCR found East Quay." {
		t.Fatalf("OCR output=%q", got)
	}
}

func TestProcessImageOCRTreatsAMissingExecutableAsUnavailable(t *testing.T) {
	provider := NewProcessImageOCR(ProcessImageOCRConfig{Binary: "not-a-real-overwatch-ocr"})
	_, err := provider.Extract(context.Background(), RetainedCapture{MediaType: "image/png", Bytes: []byte("image")})
	if !errors.Is(err, ErrOCRUnavailable) {
		t.Fatalf("error=%v, want ErrOCRUnavailable", err)
	}
}

type fixedExtractionClock struct{ at time.Time }

func (c fixedExtractionClock) Now() time.Time { return c.at }

func onePagePDF(text string) []byte {
	text = strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(text)
	stream := "BT\n/F1 18 Tf\n72 720 Td\n(" + text + ") Tj\nET\n"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var document bytes.Buffer
	document.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = document.Len()
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := document.Len()
	fmt.Fprintf(&document, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&document, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&document, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return document.Bytes()
}
