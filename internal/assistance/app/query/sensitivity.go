package query

func validMaxSensitivity(value string) bool {
	return value == "internal" || value == "restricted"
}
