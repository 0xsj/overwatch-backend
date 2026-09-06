package provenance

import "strconv"

type Origin uint8

const (
	OriginUnknown Origin = iota
	OriginRequest
	OriginSchedule
	OriginReplay
	OriginBackfill
	OriginStartup
)

var Origins = []Origin{OriginRequest, OriginSchedule, OriginReplay, OriginBackfill, OriginStartup}

var originNames = [...]string{
	OriginUnknown:  "unknown",
	OriginRequest:  "request",
	OriginSchedule: "schedule",
	OriginReplay:   "replay",
	OriginBackfill: "backfill",
	OriginStartup:  "startup",
}

func (o Origin) String() string {
	if int(o) < len(originNames) && originNames[o] != "" {
		return originNames[o]
	}
	return "origin(" + strconv.Itoa(int(o)) + ")"
}

func ParseOrigin(s string) (Origin, bool) {
	for o, known := range originNames {
		if known != "" && known == s {
			return Origin(o), true
		}
	}
	return OriginUnknown, false
}

func (o Origin) known() bool {
	for _, k := range Origins {
		if o == k {
			return true
		}
	}
	return false
}
