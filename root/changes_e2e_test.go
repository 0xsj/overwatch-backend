package root

import (
	"net/http"
	"testing"

	orgdomain "github.com/0xsj/overwatch-backend/internal/org/domain"
)

type seenWatermarkResponse struct {
	SeenAt string `json:"seen_at"`
}

func TestChangesSeenWatermarkIsDurableAndAccountScoped(t *testing.T) {
	s := tracedSystem(t)
	_, workspace, _, _, ownerAuth, otherAuth := firm(t, s, orgdomain.RoleMember)
	path := "/v1/workspaces/" + workspace.String() + "/changes"

	var before seenWatermarkResponse
	if res := s.get(t, path, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("read changes before marking: %d", res.StatusCode)
	} else {
		decode(t, res, &before)
	}
	if before.SeenAt != "" {
		t.Fatalf("a new account already had a watermark: %q", before.SeenAt)
	}

	var marked seenWatermarkResponse
	if res := s.post(t, path+"/seen", "", ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("mark changes seen: %d", res.StatusCode)
	} else {
		decode(t, res, &marked)
	}
	if marked.SeenAt == "" {
		t.Fatal("marking changes returned no server timestamp")
	}

	var after seenWatermarkResponse
	if res := s.get(t, path, ownerAuth); res.StatusCode != http.StatusOK {
		t.Fatalf("read changes after marking: %d", res.StatusCode)
	} else {
		decode(t, res, &after)
	}
	if after.SeenAt != marked.SeenAt {
		t.Fatalf("watermark did not persist: got %q, marked %q", after.SeenAt, marked.SeenAt)
	}

	// The second account belongs to the organisation but has no workspace grant;
	// it cannot read or write the first account's marker.
	if res := s.get(t, path, otherAuth); res.StatusCode != http.StatusNotFound {
		t.Fatalf("ungranted account read changes: %d", res.StatusCode)
	}
	if res := s.post(t, path+"/seen", "", otherAuth); res.StatusCode != http.StatusNotFound {
		t.Fatalf("ungranted account wrote changes marker: %d", res.StatusCode)
	}
}
