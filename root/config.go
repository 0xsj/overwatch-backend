package root

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

	// BaseURL is the CLIENT's origin, not this server's. It is what a
	// verification link points at, and a link to the wrong host arrives, looks
	// correct and does nothing — so mail.New refuses to construct without it and
	// the process dies here rather than at the first registration.
	BaseURL  string
	MailAddr string
	MailFrom string

	// JournalRetentionDays is how long a journal WORK line survives —
	// decisions/0022. Decisions are never swept. Below the floor the process
	// refuses to boot, because this is the one setting whose mistake deletes
	// evidence irreversibly.
	JournalRetentionDays int
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
		slog.String("base_url", c.BaseURL),
		slog.String("mail_addr", c.MailAddr),
		slog.String("mail_from", c.MailFrom),
		slog.Int("journal_retention_days", c.JournalRetentionDays),
	)
}

func loadConfig(lookup env.Lookup) (Config, []env.Var, error) {
	r := env.New(lookup)
	c := Config{
		ServerPort:  r.RequiredInt("PORT_SERVER"),
		DatabaseURL: r.Secret("DATABASE_URL"),
		LogLevel:    r.Enum("LOG_LEVEL", "info", "debug", "info", "warn", "error"),
		BaseURL:     r.Required("BASE_URL"),
		MailAddr:    r.Required("MAIL_ADDR"),
		MailFrom:    r.String("MAIL_FROM", "Overwatch <no-reply@overwatch.test>"),
		// Days rather than a duration string: an operator writing `90` cannot
		// mean 90 nanoseconds, and `90s` in a Go duration field is the unit typo
		// decisions/0022's floor exists to catch.
		JournalRetentionDays: r.Int("JOURNAL_RETENTION_DAYS", 90),
	}
	if err := r.Err(); err != nil {
		return Config{}, nil, err
	}
	return c, r.Declared(), nil
}
