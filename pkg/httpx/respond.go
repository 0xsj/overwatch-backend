package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// StatusClientClosed is nginx's convention for a request whose caller went away.
// It is not an IANA code and never reaches a client — nobody is listening — but
// it is the honest value in a log line, where 500 would read as our fault.
const StatusClientClosed = 499

func Status(kind errors.Kind) int {
	switch kind {
	case errors.Invalid:
		return http.StatusBadRequest
	case errors.Unauthenticated:
		return http.StatusUnauthorized
	case errors.Forbidden:
		return http.StatusForbidden
	case errors.NotFound:
		return http.StatusNotFound
	case errors.Conflict:
		return http.StatusConflict
	case errors.PreconditionFailed:
		return http.StatusPreconditionFailed
	case errors.Unprocessable:
		return http.StatusUnprocessableEntity
	case errors.PreconditionRequired:
		return http.StatusPreconditionRequired
	case errors.RateLimited:
		return http.StatusTooManyRequests
	case errors.Unavailable:
		return http.StatusServiceUnavailable
	case errors.Timeout:
		return http.StatusGatewayTimeout
	case errors.Canceled:
		return StatusClientClosed
	default:
		return http.StatusInternalServerError
	}
}

type problem struct {
	Kind      string            `json:"kind"`
	Message   string            `json:"message"`
	Type      string            `json:"type,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}

func WriteJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	// Marshal BEFORE the status. json.Encoder buffers anyway — streaming is not
	// what writing the header first buys — so the only thing that ordering cost
	// was the ability to report an encoding failure at all. It produced a 200
	// with a zero-byte body, measured. Marshalling first keeps the 500
	// available, at the same memory.
	var buf []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"kind":"internal","message":"internal error"}`))
			return
		}
		buf = b
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if buf == nil || r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(buf)
	_, _ = w.Write([]byte{'\n'})
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	kind := errors.KindOf(err)
	p := problem{
		Kind:    kind.String(),
		Message: errors.Message(err),
		// A domain slug, when the error carries one. It is the stable thing an
		// integrator branches on, and it was declared here and never assigned.
		Type:   errors.TypeOf(err),
		Fields: errors.FieldsOf(err),
	}
	writeProblemValue(w, r, Status(kind), p)
}

// Fail logs the whole chain once and answers with the caller-safe half. The
// split is pkg/errors' rule — return or log, never both — and the edge is the
// one place that does the logging.
func Fail(log *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	attrs := []any{"cause", err, "method", r.Method, "path", r.URL.Path}
	// DetailsOf is the diagnostic half — a constraint name, a SQLSTATE, the
	// subject of a refusal — and this is the only place allowed to read it.
	// pkg/postgres attaches them citing exactly this reader; without it three
	// files are each correct and meet nowhere.
	for k, v := range errors.DetailsOf(err) {
		attrs = append(attrs, k, v)
	}
	log.ErrorContext(r.Context(), "request failed", attrs...)
	WriteError(w, r, err)
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, kind, msg string, fields map[string]string) {
	writeProblemValue(w, r, status, problem{Kind: kind, Message: msg, Fields: fields})
}

func writeProblemValue(w http.ResponseWriter, r *http.Request, status int, p problem) {
	if cur, ok := provenance.Current(r.Context()); ok {
		p.RequestID = cur.Request().String()
	}
	WriteJSON(w, r, status, p)
}
