package errors

import "strconv"

type Kind uint8

const (
	Internal Kind = iota
	Invalid
	NotFound
	Conflict
	Unauthenticated
	Forbidden
	RateLimited
	Unavailable
	Timeout
	Canceled
	Unprocessable
	PreconditionFailed
	PreconditionRequired
)

var Kinds = []Kind{
	Internal,
	Invalid,
	NotFound,
	Conflict,
	Unauthenticated,
	Forbidden,
	RateLimited,
	Unavailable,
	Timeout,
	Canceled,
	Unprocessable,
	PreconditionFailed,
	PreconditionRequired,
}

var kindNames = [...]string{
	Internal:             "internal",
	Invalid:              "invalid",
	NotFound:             "not_found",
	Conflict:             "conflict",
	Unauthenticated:      "unauthenticated",
	Forbidden:            "forbidden",
	RateLimited:          "rate_limited",
	Unavailable:          "unavailable",
	PreconditionFailed:   "precondition_failed",
	PreconditionRequired: "precondition_required",
	Timeout:              "timeout",
	Canceled:             "canceled",
	Unprocessable:        "unprocessable",
}

func (k Kind) String() string {
	if int(k) < len(kindNames) && kindNames[k] != "" {
		return kindNames[k]
	}
	return "kind(" + strconv.Itoa(int(k)) + ")"
}

func ParseKind(name string) (Kind, bool) {
	for kind, known := range kindNames {
		if known != "" && known == name {
			return Kind(kind), true
		}
	}
	return Internal, false
}

func (k Kind) Retryable() bool {
	return k == Unavailable || k == RateLimited || k == Timeout
}
