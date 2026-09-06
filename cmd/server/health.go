package main

import (
	"net/http"

	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// live answers whether the process is running, and touches nothing else.
//
// It must not check the database. An orchestrator kills a process that fails
// liveness, so wiring a dependency into it means a database blip restarts every
// healthy instance at once — turning a degradation into an outage, and doing it
// hardest at the moment the database can least afford the reconnect storm.
func (a *app) live(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

// ready answers whether this instance can serve a request, which means its
// dependencies are reachable. Failing it removes the instance from rotation and
// nothing more, which is the recoverable half of the pair.
func (a *app) ready(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// The ping is a unit of work CAUSED by this request, so it derives rather
	// than reusing the request's own identity. That is what makes the two lines
	// in the log a tree instead of a pair: same correlation, causation pointing
	// back at the request, depth one deeper. Derive fails only past MaxDepth,
	// which cannot happen one hop from an origin.
	if cur, ok := provenance.Current(ctx); ok {
		if child, err := cur.Derive(a.ids); err == nil {
			ctx = provenance.NewContext(ctx, child)
			a.log.InfoContext(ctx, "checking the database")
		}
	}

	if err := a.db.Ping(ctx); err != nil {
		// Fail through the normal path: the kind decides the status, the body
		// says nothing an attacker can use, and the request id ties it to the
		// log line carrying the real cause.
		httpx.Fail(a.log, w, r.WithContext(ctx), err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
}
