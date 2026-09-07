package domain

import (
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Stream is which of a process's two outputs these bytes are.
//
// **They are two artifacts, not one.** `pkg/execx` caps and truncates each
// separately, on the argument that "a tool that fills stderr with warnings has
// not lost its findings" — and a single concatenated artifact would make that
// distinction unrecoverable after the fact.
type Stream uint8

const (
	StreamStdout Stream = iota
	StreamStderr
)

func (s Stream) String() string {
	if s == StreamStderr {
		return "stderr"
	}
	return "stdout"
}

func ParseStream(s string) (Stream, error) {
	switch s {
	case "stdout":
		return StreamStdout, nil
	case "stderr":
		return StreamStderr, nil
	default:
		return StreamStdout, ErrStreamUnknown
	}
}

// Artifact is the ROW; pkg/blob holds the bytes.
//
// The row is what a citation points at and the hash is what makes the citation
// checkable: re-hash the bytes and compare, and a record citing bytes that no
// longer hash to their name has stopped being true. That is the difference
// between an archive and a claim.
//
// **This domain never deletes bytes.** `pkg/blob` has no delete at all, and
// retention is a policy about the record — removing bytes another row still
// cites turns a citation into a lie.
type Artifact struct {
	ID           id.ID
	WorkspaceID  id.ID
	InvocationID id.ID

	Stream Stream

	// Hash is `sha256:<hex>` — pkg/blob's Ref, spelled. The algorithm travels
	// with it so the day sha256 is not enough there is a field to widen rather
	// than a convention to find.
	Hash string

	// Bytes is the size, and ZERO IS A RESULT: it ran and wrote an empty
	// artifact. "Nothing was written" is the ABSENCE of this row, not a zero in
	// it — CLAUDE.md's rule that an unmeasured total renders as `–`.
	Bytes int64

	// Truncated says the stream hit execx's cap. It is a fact about the
	// artifact rather than something to infer from a length, because after the
	// fact a length and a limit look identical.
	Truncated bool

	// MediaType is what the bytes are said to be, and it is never sniffed. A
	// tool invoked with `-json` produces JSON; that is a fact about the argv,
	// not about the first bytes, and guessing it from content is how a record
	// acquires a claim nobody made.
	MediaType string

	CreatedAt time.Time
}

func NewArtifact(newID, workspace, invocation id.ID, stream Stream,
	hash string, size int64, truncated bool, mediaType string, at time.Time) (Artifact, error) {
	if newID.IsZero() || invocation.IsZero() {
		return Artifact{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Artifact{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Artifact{}, ErrTimeRequired
	}
	if hash == "" {
		return Artifact{}, ErrHashRequired
	}
	if size < 0 {
		return Artifact{}, ErrHashRequired
	}
	return Artifact{
		ID: newID, WorkspaceID: workspace, InvocationID: invocation,
		Stream: stream, Hash: hash, Bytes: size, Truncated: truncated,
		MediaType: mediaType, CreatedAt: at,
	}, nil
}

// Empty is the distinction that has to survive to the screen: it ran and wrote
// nothing, which is not the same as nothing having been written.
func (a Artifact) Empty() bool { return a.Bytes == 0 }
