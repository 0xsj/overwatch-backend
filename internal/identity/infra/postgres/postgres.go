package postgres

import (
	"embed"

	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "identity"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("identity: " + err.Error())
	}
	return ms
}

type Store struct {
	db *postgres.Pool
}

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("identity: NewStore with a nil Pool")
	}
	return &Store{db: db}
}
