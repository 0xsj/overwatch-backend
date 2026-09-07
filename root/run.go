package root

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"syscall"
	"time"

	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
)

func Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := Boot(ctx)
	if err != nil {
		return err
	}
	defer a.close()

	srv := &http.Server{
		Addr:              net.JoinHostPort("", strconv.Itoa(a.cfg.ServerPort)),
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		// No WriteTimeout: it is a deadline on the whole response, so it
		// truncates a long download or a stream rather than protecting anything.
		// Per-request deadlines belong in handlers, where the work is known.
		BaseContext: func(net.Listener) context.Context { return a.boot },
	}

	// Bind before announcing. ListenAndServe binds and serves in one call, so a
	// bind failure arrives AFTER the "listening" line has already been printed —
	// a log that states the one thing that did not happen.
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return pkgerrors.Wrap(err, pkgerrors.Unavailable, "cannot listen on "+srv.Addr).
			WithField("port", strconv.Itoa(a.cfg.ServerPort)).
			WithDetail("hint", "something already holds it — `lsof -nP -iTCP:"+
				strconv.Itoa(a.cfg.ServerPort)+" -sTCP:LISTEN`. On macOS, 7000 and 5000 are AirPlay Receiver")
	}

	// The dispatcher is a second lifecycle beside the listener, not a goroutine
	// somebody starts and forgets. It polls until the signal context is
	// cancelled; the drain below is what stops the process exiting with events
	// committed and nobody having tried to deliver them.
	dispatched := make(chan struct{})
	go func() {
		defer close(dispatched)
		if err := a.dispatcher.Run(ctx); err != nil {
			a.log.ErrorContext(a.boot, "dispatcher stopped", "cause", err)
		}
	}()

	// The sweep is a third lifecycle. It is started rather than merely
	// constructed, because decisions/0022 names "a sweeper that is constructed
	// and never started" as the failure that looks exactly like working: a
	// table that is not shrinking looks like nothing at all.
	swept := make(chan struct{})
	go func() {
		defer close(swept)
		a.sweeper.Run(ctx)
	}()

	errc := make(chan error, 1)
	go func() {
		a.log.InfoContext(a.boot, "listening", "addr", ln.Addr().String())
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return pkgerrors.Wrap(err, pkgerrors.Unavailable, "server stopped")
	case <-ctx.Done():
	}

	// Stop accepting, let in-flight requests finish, then give up. A shutdown
	// that waits forever is a process an orchestrator kills anyway, and one that
	// does not wait at all drops a response somebody is reading.
	a.log.InfoContext(a.boot, "shutting down")
	shutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return pkgerrors.Wrap(err, pkgerrors.Timeout, "shutdown did not finish")
	}

	// Requests have stopped, so nothing new is being committed. Wait for the
	// poll loop to notice the cancellation, then empty the table — in that
	// order, or the drain races the loop for the same rows.
	<-dispatched
	// The sweep is NOT drained. It deletes rows nobody is waiting for, and a
	// pass interrupted mid-batch leaves the rows it already deleted deleted —
	// the next start recomputes the cutoff and carries on. Waiting for it would
	// hold shutdown open for a scan that has no deadline of its own.
	<-swept
	if err := a.dispatcher.Drain(shutCtx); err != nil {
		return pkgerrors.Wrap(err, pkgerrors.Unavailable, "the outbox did not drain")
	}
	a.log.InfoContext(a.boot, "drained")
	return nil
}

// report writes to a terminal, where there is no untrusted caller — so it
// prints the FULL chain, not errors.Message. Message exists to hide an internal
// cause from a caller over the wire; using it here hid "address already in use"
// behind "server stopped" and cost an afternoon.
func Report(err error) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", pkgerrors.KindOf(err), err)
	print := func(label string, m map[string]string) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(os.Stderr, "  %s %s: %s\n", label, k, m[k])
		}
	}
	print("·", pkgerrors.FieldsOf(err))
	print("·", pkgerrors.DetailsOf(err))
}
