package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestExtractionRequiresOutputOnlyWhenSucceeded(t *testing.T) {
	ids := id.NewSequence(time.Unix(1, 0))
	at := time.Unix(2, 0)
	got, err := New(ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), MethodPDFV1, Succeeded, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", 42, "", at)
	if err != nil || got.Status != Succeeded || got.OutputBytes != 42 {
		t.Fatalf("succeeded extraction: %+v err=%v", got, err)
	}
	if _, err := New(ids.NewID(), got.WorkspaceID, got.SourceID, got.CaptureID, got.CreatedBy, MethodPDFV1, Unsupported, "", 0, "images need OCR", at); err != nil {
		t.Fatal(err)
	}
	if _, err := New(ids.NewID(), got.WorkspaceID, got.SourceID, got.CaptureID, got.CreatedBy, MethodPDFV1, Failed, "", 0, "decoder failed", at); err != nil {
		t.Fatal(err)
	}
}

func TestExtractionRejectsMixedSuccessAndFailureShape(t *testing.T) {
	ids := id.NewSequence(time.Unix(1, 0))
	if _, err := New(ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), MethodPDFV1, Failed, "hash", 4, "decoder failed", time.Unix(2, 0)); err != ErrTextInvalid {
		t.Fatalf("mixed output: %v", err)
	}
}
