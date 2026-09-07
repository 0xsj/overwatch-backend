package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	command "github.com/0xsj/overwatch-backend/internal/identity/app/command"
	"github.com/0xsj/overwatch-backend/internal/identity/app/query"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/limit"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

type API struct {
	registrar *command.Registrar
	auth      *command.Authenticator
	verifier  *command.Verifier
	settings  *command.Settings
	sessions  *query.Sessions
	// mail limits the two endpoints that send one. Nil disables the charge,
	// which is a test's business and never a deployment's.
	mail *limit.Limiter
	log  *slog.Logger
}

func NewAPI(registrar *command.Registrar, auth *command.Authenticator,
	verifier *command.Verifier, settings *command.Settings, sessions *query.Sessions,
	mail *limit.Limiter, log *slog.Logger) *API {
	if registrar == nil || auth == nil || verifier == nil || settings == nil || sessions == nil {
		panic("identity: NewAPI with a nil dependency")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &API{registrar: registrar, auth: auth, verifier: verifier,
		settings: settings, sessions: sessions, mail: mail, log: log}
}

// Routes names resources, not verbs. `POST /v1/accounts` creates an account the
// way `POST /v1/sessions` creates a session; `/v1/register` was a verb and is
// gone. A token always travels in the BODY — a token in a path or a query is
// written to every access log it passes and leaks onward in a Referer header,
// and neither of those is under this server's control.
func (a *API) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/accounts", a.register)
	mux.HandleFunc("POST /v1/sessions", a.signIn)
	mux.HandleFunc("DELETE /v1/sessions/current", a.signOut)
	mux.HandleFunc("POST /v1/verifications", a.requestVerification)
	mux.HandleFunc("POST /v1/verifications/confirm", a.confirmVerification)
	mux.HandleFunc("POST /v1/password-resets", a.requestReset)
	mux.HandleFunc("POST /v1/password-resets/confirm", a.confirmReset)

	// Account settings — decisions/0021. Everything under /v1/me acts on the
	// caller and takes no account id, because none of it is a way to act on
	// somebody else.
	mux.HandleFunc("PATCH /v1/me", a.rename)
	mux.HandleFunc("POST /v1/me/password", a.changePassword)
	mux.HandleFunc("POST /v1/me/email", a.changeEmail)
	mux.HandleFunc("POST /v1/email-changes/confirm", a.confirmEmail)
	mux.HandleFunc("GET /v1/me/sessions", a.mySessions)
	mux.HandleFunc("DELETE /v1/me/sessions/{session}", a.endSession)
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type registerResponse struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Status    string `json:"status"`
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var in registerRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		httpx.WriteError(w, r, errors.Wrap(err, errors.Invalid, "the request body could not be read"))
		return
	}

	out, err := a.registrar.Register(r.Context(), command.Registration{
		Email:    in.Email,
		Password: secret.New(in.Password),
		Name:     in.Name,
	})
	if err != nil {
		httpx.Fail(a.log, w, r, err)
		return
	}

	httpx.WriteJSON(w, r, http.StatusCreated, registerResponse{
		AccountID: out.Account.ID.String(),
		Email:     out.Account.Email.String(),
		Status:    out.Account.Status.String(),
	})
}
