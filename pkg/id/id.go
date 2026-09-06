package id

import (
	"encoding/hex"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

type ID [16]byte

var Nil ID

var hyphens = [...]int{8, 13, 18, 23}

func (i ID) IsZero() bool { return i == Nil }

func (i ID) Version() int { return int(i[6] >> 4) }

func (i ID) Time() (time.Time, bool) {
	if i.Version() != 7 {
		return time.Time{}, false
	}
	ms := int64(i[0])<<40 | int64(i[1])<<32 | int64(i[2])<<24 |
		int64(i[3])<<16 | int64(i[4])<<8 | int64(i[5])
	return time.UnixMilli(ms).UTC(), true
}

func (i ID) String() string {
	var b [36]byte
	hex.Encode(b[0:8], i[0:4])
	b[8] = '-'
	hex.Encode(b[9:13], i[4:6])
	b[13] = '-'
	hex.Encode(b[14:18], i[6:8])
	b[18] = '-'
	hex.Encode(b[19:23], i[8:10])
	b[23] = '-'
	hex.Encode(b[24:36], i[10:16])
	return string(b[:])
}

func (i ID) MarshalText() ([]byte, error) { return []byte(i.String()), nil }

func (i *ID) UnmarshalText(b []byte) error {
	parsed, err := Parse(string(b))
	if err != nil {
		return err
	}
	*i = parsed
	return nil
}

func Parse(s string) (ID, error) {
	reject := func(problem string) (ID, error) {
		return Nil, errors.Newf(errors.Invalid, "not an identifier: %q", s).
			WithField("id", problem)
	}
	if len(s) != 36 {
		return reject("must be 36 characters")
	}
	for _, p := range hyphens {
		if s[p] != '-' {
			return reject("hyphens must be at 8, 13, 18 and 23")
		}
	}
	var packed [32]byte
	n := 0
	for j := 0; j < len(s); j++ {
		if s[j] == '-' {
			continue
		}
		packed[n] = s[j]
		n++
	}
	var out ID
	if _, err := hex.Decode(out[:], packed[:]); err != nil {
		return reject("must be hexadecimal")
	}
	if out == Nil {
		return reject("the nil identifier names nothing")
	}
	return out, nil
}
