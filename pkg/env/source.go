package env

import (
	"os"
	"strconv"
	"strings"
)

type Lookup func(key string) (value string, present bool)

func OS() Lookup { return os.LookupEnv }

func Map(m map[string]string) Lookup {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

type Var struct {
	Key     string
	Value   string
	Default string
	Set     bool
	Secret  bool
}

func (v Var) String() string {
	value := v.Value
	if value == "" || strings.ContainsAny(value, " \t\"") {
		value = strconv.Quote(value)
	}
	if v.Set {
		return v.Key + "=" + value
	}
	return v.Key + "=" + value + " (default)"
}
