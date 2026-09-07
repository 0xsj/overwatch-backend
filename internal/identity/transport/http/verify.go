package http

import (
	"encoding/json"
	"net/http"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

type addressRequest struct {
	Email string `json:"email"`
}

type tokenRequest struct {
	Token string `json:"token"`
}

type resetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (a *API) requestVerification(w http.ResponseWriter, r *http.Request) {
	var in addressRequest
	if !decode(a, w, r, &in) {
		return
	}
	if !a.spend(w, r, "verify:") {
		return
	}
	if err := a.verifier.RequestVerification(r.Context(), in.Email); err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) confirmVerification(w http.ResponseWriter, r *http.Request) {
	var in tokenRequest
	if !decode(a, w, r, &in) {
		return
	}
	account, err := a.verifier.Verify(r.Context(), secret.New(in.Token))
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, registerResponse{
		AccountID: account.ID.String(),
		Email:     account.Email.String(),
		Status:    account.Status.String(),
	})
}

func (a *API) requestReset(w http.ResponseWriter, r *http.Request) {
	var in addressRequest
	if !decode(a, w, r, &in) {
		return
	}
	if !a.spend(w, r, "reset:") {
		return
	}
	if err := a.verifier.RequestReset(r.Context(), in.Email); err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) confirmReset(w http.ResponseWriter, r *http.Request) {
	var in resetRequest
	if !decode(a, w, r, &in) {
		return
	}
	if err := a.verifier.CompleteReset(r.Context(), secret.New(in.Token), secret.New(in.Password)); err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// spend charges the caller's address for an act that sends mail. Without it
// these two endpoints are an unauthenticated way to post mail to any address, at
// whatever rate a script can manage, from this product's domain.
//
// The key is prefixed per endpoint so exhausting one does not close the other:
// somebody unable to receive their verification mail must still be able to reset
// a password.
func (a *API) spend(w http.ResponseWriter, r *http.Request, prefix string) bool {
	if a.mail == nil {
		return true
	}
	if a.mail.Allow(prefix + clientAddress(r)) {
		return true
	}
	httpx.WriteError(w, r, errors.New(errors.RateLimited, "too many requests — try again shortly"))
	return false
}

func decode(a *API, w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		httpx.WriteError(w, r, errors.Wrap(err, errors.Invalid, "the request body could not be read"))
		return false
	}
	return true
}
