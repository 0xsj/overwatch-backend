package root

import (
	"net/http"

	"github.com/0xsj/overwatch-backend/pkg/httpx"
)

// routes is the whole surface. Middleware order is a contract, not a style, and
// it fails by OMISSION — swapping two entries drops fields silently and nothing
// checks it:
//
//	recovery     outermost. It must cover the provenance layer itself, because
//	             the Identifier that layer calls is injected and does real work —
//	             a token parse, a database lookup — and a panic there would
//	             otherwise escape every recover, dropping the connection with no
//	             500 and no log line
//	provenance   inside recovery, and it installs the chain IN PLACE so the
//	             recovery above still names the request it died on. Its
//	             Identifier resolves the bearer token, which is what makes an
//	             audit entry name a person rather than `anonymous` — and it is
//	             SILENT on a bad token, because provenance is metadata and must
//	             never be the thing that authorises
//	logging      innermost, so it measures the handler and not the middleware
func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.live)
	mux.HandleFunc("GET /readyz", a.ready)
	a.identity.Routes(mux)

	return httpx.Chain(mux,
		httpx.WithRecovery(a.log),
		httpx.WithProvenance(a.ids, a.whoami),
		// Liveness is polled every few seconds forever. Logging it buries
		// everything else and tells nobody anything.
		httpx.WithLogging(a.log, "/healthz"),
	)
}
