package domain

import "strings"

// LinePath is the path a TEXT artifact's one value is at.
//
// A line of `subfinder -silent` output is a bare hostname: it has no key,
// because the format has no keys. Extraction still has to name where the value
// was, so it synthesises the only honest name there is — it was on a line.
//
// **This is a path, not a field name.** `.line` says WHERE the value sat in the
// bytes; what it MEANS is still the mapping's `field`, exactly as it is for
// `.host` in a JSON record. The rule the package doc calls the most tempting one
// to break — never name a field from the source's own key — is untouched here,
// because a text line has no key to be tempted by.
const LinePath = "line"

// Shape is what the bytes ARE, and it comes from the invocation's declared
// media type — never from looking at the content.
//
// **Extraction ignored this until 2026-09-08 and assumed JSON everywhere.** The
// cost was silent and total: `subfinder -silent` emitted 487KB of perfectly good
// hostnames, every line failed `json.Unmarshal`, `records` skipped it as the
// banner case, and the run finished green with `fields_seen: 0`. Nothing was
// wrong with the tool, the mapping, the scope rule or the target — the reader
// was looking for objects in a file that has never contained one.
//
// The two-value vocabulary is deliberate. A third shape — CSV, XML — is a real
// question about a real tool, and the answer is a case here rather than a
// content sniff, for the same reason `root/runports.go` reads the media type off
// the ARGV: what the bytes are is a fact about the command that produced them.
type Shape uint8

const (
	// ShapeJSON is the zero value, so a caller that has not been taught about
	// shapes yet reads what extraction has always read.
	ShapeJSON Shape = iota
	// ShapeLines is one record per line, each carrying a single value at
	// [LinePath]. It is what most of the recon corpus emits: subfinder,
	// assetfinder, dnsx, waybackurls and httpx without `-json` all write one
	// identifier per line and nothing else.
	ShapeLines
)

// ShapeOf reads the artifact's declared media type.
//
// **An EMPTY media type is JSON**, which is the shape extraction assumed before
// it asked. An unknown type is not evidence of text, and defaulting an
// unrecognised one to lines would turn a JSON artifact into one `.line` path per
// record — wrong in a way that looks like a working extraction with a strange
// schema.
func ShapeOf(mediaType string) Shape {
	kind, _, _ := strings.Cut(mediaType, ";")
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "text/plain":
		return ShapeLines
	default:
		return ShapeJSON
	}
}
