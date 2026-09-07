package http

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/identity/app/command"
	"github.com/0xsj/overwatch-backend/internal/identity/app/query"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

const bearer = "Bearer "

type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type signInResponse struct {
	Token     string `json:"token"`
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expires_at"`
}

func (a *API) signIn(w http.ResponseWriter, r *http.Request) {
	var in signInRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		httpx.WriteError(w, r, errors.Wrap(err, errors.Invalid, "the request body could not be read"))
		return
	}

	out, err := a.auth.SignIn(r.Context(), command.Credentials{
		Email:    in.Email,
		Password: secret.New(in.Password),
		// Neither is trusted and neither is used for a decision. They exist so a
		// person reading their own session list can recognise one they do not.
		UserAgent: r.UserAgent(),
		Address:   clientAddress(r),
	})
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}

	// The token is written here and nowhere else — not logged, not echoed into
	// a header, not stored anywhere this process can read again.
	httpx.WriteJSON(w, r, http.StatusCreated, signInResponse{
		Token:     out.Token.Reveal(),
		AccountID: out.Account.ID.String(),
		Email:     out.Account.Email.String(),
		Status:    out.Account.Status.String(),
		ExpiresAt: out.Session.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (a *API) signOut(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.SignOut(r.Context(), Presented(r)); err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Presented pulls the token out of a request. One place, because a second
// reader that accepts a query parameter or a cookie is a second attack surface
// nobody audited.
func Presented(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > len(bearer) && strings.EqualFold(h[:len(bearer)], bearer) {
		return strings.TrimSpace(h[len(bearer):])
	}
	return ""
}

// Identifier is what fills provenance's actor. Until this is wired every record
// in the system names `anonymous`, which is the state the product spent its
// first week in.
//
// It is DELIBERATELY silent on failure. A bad token here yields the anonymous
// actor and the request continues to a handler that will refuse it properly —
// provenance is metadata and must never be the thing that authorises, or a
// forgotten check becomes a security failure rather than a missing log field.
func Identifier(sessions *query.Sessions) httpx.Identifier {
	return func(r *http.Request) provenance.Actor {
		token := Presented(r)
		if token == "" {
			return provenance.Anonymous()
		}
		caller, err := sessions.Authenticate(r.Context(), token)
		if err != nil {
			return provenance.Anonymous()
		}
		actor, err := provenance.User(caller.AccountID.String())
		if err != nil {
			return provenance.Anonymous()
		}
		return actor
	}
}

// clientAddress is the peer, deliberately without consulting X-Forwarded-For.
// httpx.ClientIP takes a trusted-proxy set precisely because that header is
// written by whoever sent the request, and this value is only ever RENDERED —
// on a person's own session list, so they can recognise one they do not. A
// header believed here would let an attacker write whatever they liked into
// somebody else's screen.
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
