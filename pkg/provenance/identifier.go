package provenance

func validID(s string) bool {
	if len(s) == 0 || len(s) > MaxIDLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !validIDByte(s[i]) {
			return false
		}
	}
	return true
}

func validIDByte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z',
		c >= 'a' && c <= 'z',
		c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '.', '_', ':', '/', '-':
		return true
	}
	return false
}

const traceparentLength = 55

func validTraceparent(s string) bool {
	if len(s) != traceparentLength {
		return false
	}
	if s[2] != '-' || s[35] != '-' || s[52] != '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 2 || i == 35 || i == 52 {
			continue
		}
		if !lowerHex(s[i]) {
			return false
		}
	}
	if s[0] == 'f' && s[1] == 'f' {
		return false
	}
	if allZero(s[3:35]) || allZero(s[36:52]) {
		return false
	}
	return true
}

func lowerHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

func allZero(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}
