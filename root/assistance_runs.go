package root

import (
	"net/http"
	"strconv"

	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/httpx"
)

func (m *me) listAssistanceProviderRuns(w http.ResponseWriter, r *http.Request) {
	workspace, _, ok := m.onWorkspaceResearchRead(w, r)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			httpx.Fail(m.log, w, r, pkgerrors.New(pkgerrors.Invalid, "limit is invalid"))
			return
		}
		limit = parsed
	}
	found, err := m.research.providerRuns.List(r.Context(), workspace, limit)
	if err != nil {
		httpx.Fail(m.log, w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, found)
}
