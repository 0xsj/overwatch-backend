package http

import (
	"net/http"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

type renameRequest struct {
	Name string `json:"name"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	Password        string `json:"password"`
}

type changeEmailRequest struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"current_password"`
}

type deviceResponse struct {
	SessionID string `json:"session_id"`
	UserAgent string `json:"user_agent"`
	Address   string `json:"address"`
	IssuedAt  string `json:"issued_at"`
	ExpiresAt string `json:"expires_at"`
	Current   bool   `json:"current"`
}

// caller resolves the session behind a request into the two ids every settings
// handler needs. It exists so no handler reads a token, and so "which session am
// I" is answered once.
func (a *API) caller(r *http.Request) (account, session id.ID, err error) {
	found, err := a.sessions.Authenticate(r.Context(), Presented(r))
	if err != nil {
		return id.ID{}, id.ID{}, err
	}
	return found.AccountID, found.SessionID, nil
}

func (a *API) rename(w http.ResponseWriter, r *http.Request) {
	account, _, err := a.caller(r)
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	var in renameRequest
	if !decode(a, w, r, &in) {
		return
	}
	updated, err := a.settings.Rename(r.Context(), account, in.Name)
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, registerResponse{
		AccountID: updated.ID.String(),
		Email:     updated.Email.String(),
		Status:    updated.Status.String(),
	})
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	account, session, err := a.caller(r)
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	var in changePasswordRequest
	if !decode(a, w, r, &in) {
		return
	}
	if err := a.settings.ChangePassword(r.Context(), account, session,
		secret.New(in.CurrentPassword), secret.New(in.Password)); err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	// 204 and no new token. The caller's session survives on purpose —
	// decisions/0021 — so there is nothing to hand back.
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) changeEmail(w http.ResponseWriter, r *http.Request) {
	account, _, err := a.caller(r)
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	var in changeEmailRequest
	if !decode(a, w, r, &in) {
		return
	}
	if !a.spend(w, r, "email:") {
		return
	}
	if err := a.settings.RequestEmailChange(r.Context(), account, in.Email,
		secret.New(in.CurrentPassword)); err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	// 202: nothing has changed yet, and will not until the new address confirms.
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) confirmEmail(w http.ResponseWriter, r *http.Request) {
	var in tokenRequest
	if !decode(a, w, r, &in) {
		return
	}
	account, err := a.settings.ConfirmEmailChange(r.Context(), secret.New(in.Token))
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

func (a *API) mySessions(w http.ResponseWriter, r *http.Request) {
	account, current, err := a.caller(r)
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	found, err := a.sessions.Mine(r.Context(), account, current)
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	out := make([]deviceResponse, 0, len(found))
	for _, d := range found {
		out = append(out, deviceResponse{
			SessionID: d.SessionID.String(),
			UserAgent: d.UserAgent,
			Address:   d.Address,
			IssuedAt:  d.IssuedAt.UTC().Format(time.RFC3339),
			ExpiresAt: d.ExpiresAt.UTC().Format(time.RFC3339),
			Current:   d.Current,
		})
	}
	httpx.WriteJSON(w, r, http.StatusOK, out)
}

// endSession revokes one of the caller's OWN sessions. The id is checked against
// the caller's list rather than trusted, because a session id is not a secret —
// it is rendered on this very screen — and an endpoint that revokes any id it is
// handed is a way to sign out anybody whose id you can guess or read.
func (a *API) endSession(w http.ResponseWriter, r *http.Request) {
	account, current, err := a.caller(r)
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	want, err := id.Parse(r.PathValue("session"))
	if err != nil {
		httpx.WriteError(w, r, errors.New(errors.NotFound, "session"))
		return
	}
	mine, err := a.sessions.Mine(r.Context(), account, current)
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}
	for _, d := range mine {
		if d.SessionID != want {
			continue
		}
		if err := a.auth.EndSessionByID(r.Context(), account, want); err != nil {
			httpx.Fail(a.log, w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Somebody else's session, or one already dead. NotFound for both, so the
	// endpoint cannot be used to discover whether an id is live.
	httpx.WriteError(w, r, errors.New(errors.NotFound, "session"))
}
