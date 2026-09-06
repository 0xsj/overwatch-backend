package main

import (
	"log/slog"

	"github.com/0xsj/overwatch-backend/pkg/env"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

// Config is what THIS binary needs. It lives here rather than in pkg/ because
// the settings a service requires are a fact about that service — split the
// runner out and it has no Database, while this one has no collector cadence.
// The machinery that reads them is shared; the shape is not.
type Config struct {
	ServerPort  int
	DatabaseURL secret.String
	LogLevel    string
}

// LogValue is implemented on the struct, not only on the credential inside it.
// slog resolves a LogValuer on the attribute itself and never on fields found
// inside it, so a Config logged whole would be encoded field by field and the
// password would survive into the record.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("server_port", c.ServerPort),
		slog.Any("database_url", c.DatabaseURL),
		slog.String("log_level", c.LogLevel),
	)
}

func loadConfig(lookup env.Lookup) (Config, []env.Var, error) {
	r := env.New(lookup)
	c := Config{
		ServerPort:  r.RequiredInt("PORT_SERVER"),
		DatabaseURL: r.Secret("DATABASE_URL"),
		LogLevel:    r.Enum("LOG_LEVEL", "info", "debug", "info", "warn", "error"),
	}
	if err := r.Err(); err != nil {
		return Config{}, nil, err
	}
	return c, r.Declared(), nil
}
