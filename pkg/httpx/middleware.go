package httpx

import (
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

const (
	HeaderRequestID     = "X-Request-Id"
	HeaderCorrelationID = "X-Correlation-Id"
	HeaderCausationID   = "X-Causation-Id"
	HeaderTraceparent   = "traceparent"
)

type Middleware func(http.Handler) http.Handler

type Identifier func(*http.Request) provenance.Actor

func Chain(h http.Handler, middleware ...Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

func WithProvenance(minter provenance.Minter, who Identifier) Middleware {
	if minter == nil {
		panic("httpx: WithProvenance with a nil Minter")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := provenance.New(provenance.OriginRequest, minter).Adopt(provenance.Adopted{
				Correlation: r.Header.Get(HeaderCorrelationID),
				Causation:   r.Header.Get(HeaderCausationID),
				Traceparent: r.Header.Get(HeaderTraceparent),
			})

			// Install the chain BEFORE calling who, and install it in place.
			//
			// In place, because *r is the same request an outer WithRecovery is
			// holding. r.WithContext returns a copy, so an outer recover() would
			// see a context that predates the chain and log the one line that
			// most needs a request id without one.
			//
			// Before calling who, because who is injected and does real work — a
			// token parse, a database lookup — so it is the part of this
			// middleware most likely to panic, and the recovery above must be
			// able to name the request it died on.
			*r = *r.WithContext(provenance.NewContext(r.Context(), p))

			if who != nil {
				p = p.WithActor(who(r))
				*r = *r.WithContext(provenance.NewContext(r.Context(), p))
			}

			w.Header().Set(HeaderRequestID, p.Request().String())
			w.Header().Set(HeaderCorrelationID, p.Correlation().String())

			next.ServeHTTP(w, r)
		})
	}
}

func WithRecovery(log *slog.Logger) Middleware {
	// Eagerly, like WithProvenance. A nil logger here does not fail where it is
	// wired — it fails inside the deferred recover, which replaces the original
	// panic with a nil dereference and writes no status at all. The middleware
	// that exists to report a bug destroys the evidence of it. Use logger.Nop().
	if log == nil {
		panic("httpx: WithRecovery with a nil Logger — use logger.Nop()")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &recorder{ResponseWriter: w}
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.ErrorContext(r.Context(), "handler panicked",
					"panic", v, "method", r.Method, "path", r.URL.Path)
				if rec.wrote {
					return
				}
				writeProblem(rec, r, http.StatusInternalServerError, "internal", "internal error", nil)
			}()
			next.ServeHTTP(rec, r)
		})
	}
}

func WithLogging(log *slog.Logger, quiet ...string) Middleware {
	if log == nil {
		panic("httpx: WithLogging with a nil Logger — use logger.Nop()")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			began := time.Now()
			rec := &recorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			// A quiet path is quiet while it works. Returning before the
			// recorder made a failing quiet path invisible — and a health check
			// that starts failing is the one line you most want.
			if rec.Status() < http.StatusInternalServerError && slices.Contains(quiet, r.URL.Path) {
				return
			}
			log.InfoContext(r.Context(), "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.Status(),
				"bytes", rec.written,
				"client", ClientIP(r, nil),
				"took", time.Since(began))
		})
	}
}

type recorder struct {
	http.ResponseWriter
	status  int
	written int
	wrote   bool
}

func (r *recorder) Status() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

func (r *recorder) WriteHeader(code int) {
	if r.wrote {
		return
	}
	r.status = code
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.wrote = true
	n, err := r.ResponseWriter.Write(b)
	r.written += n
	return n, err
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
