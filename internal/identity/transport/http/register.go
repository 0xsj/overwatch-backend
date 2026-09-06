package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	command "github.com/0xsj/overwatch-backend/internal/identity/app/command"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

type API struct {
	registrar *command.Registrar
	log       *slog.Logger
}

func NewAPI(registrar *command.Registrar, log *slog.Logger) *API {
	if registrar == nil {
		panic("identity: NewAPI with a nil registrar")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &API{registrar: registrar, log: log}
}

func (a *API) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/register", a.register)
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type registerResponse struct {
	AccountID   string `json:"account_id"`
	Email       string `json:"email"`
	OrgID       string `json:"org_id"`
	WorkspaceID string `json:"workspace_id"`
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
		AccountID:   out.Account.ID.String(),
		Email:       out.Account.Email.String(),
		OrgID:       out.Tenancy.OrgID.String(),
		WorkspaceID: out.Tenancy.WorkspaceID.String(),
	})
}
