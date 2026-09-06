package provenance

import (
	"strconv"
	"strings"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

type Kind uint8

const (
	KindAnonymous Kind = iota
	KindUser
	KindService
	KindSystem
)

var Kinds = []Kind{KindAnonymous, KindUser, KindService, KindSystem}

var kindNames = [...]string{
	KindAnonymous: "anonymous",
	KindUser:      "user",
	KindService:   "service",
	KindSystem:    "system",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "kind(" + strconv.Itoa(int(k)) + ")"
}

func parseKind(s string) (Kind, bool) {
	for k, known := range kindNames {
		if known != "" && known == s {
			return Kind(k), true
		}
	}
	return KindAnonymous, false
}

type Actor struct {
	kind Kind
	id   string
}

func Anonymous() Actor { return Actor{} }

func User(id string) (Actor, error) { return newActor(KindUser, id) }

func Service(name string) (Actor, error) { return newActor(KindService, name) }

func System(path string) (Actor, error) { return newActor(KindSystem, path) }

func newActor(kind Kind, ident string) (Actor, error) {
	if kind == KindAnonymous {
		return Actor{}, errors.New(errors.Internal,
			"provenance: use Anonymous for the anonymous actor")
	}
	if !validID(ident) {
		return Actor{}, errors.Newf(errors.Internal,
			"provenance: %s actor id %q is not 1-%d characters of [A-Za-z0-9._:/-]",
			kind, ident, MaxIDLength)
	}
	return Actor{kind: kind, id: ident}, nil
}

func ParseActor(s string) (Actor, error) {
	if s == kindNames[KindAnonymous] {
		return Anonymous(), nil
	}
	name, ident, found := strings.Cut(s, ":")
	if !found {
		return Actor{}, errors.Newf(errors.Internal,
			"provenance: actor %q is not kind:id", s)
	}
	kind, ok := parseKind(name)
	if !ok {
		return Actor{}, errors.Newf(errors.Internal,
			"provenance: actor %q names no kind", s)
	}
	return newActor(kind, ident)
}

func (a Actor) Kind() Kind { return a.kind }

func (a Actor) ID() string { return a.id }

func (a Actor) IsZero() bool { return a.kind == KindAnonymous && a.id == "" }

func (a Actor) String() string {
	if a.IsZero() {
		return kindNames[KindAnonymous]
	}
	return a.kind.String() + ":" + a.id
}
