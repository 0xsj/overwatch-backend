package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func read(ctx context.Context, err error, op string, absent error) error {
	translated := postgres.Translate(ctx, err, op)
	if errors.IsKind(translated, errors.NotFound) {
		return fmt.Errorf("%s: %w", op, absent)
	}
	return translated
}

func saved(ctx context.Context, rows int64, err error, op string, exists func(context.Context) error) error {
	if err != nil {
		return postgres.Translate(ctx, err, op)
	}
	if rows > 0 {
		return nil
	}
	if err := exists(ctx); err != nil {
		return err
	}
	return fmt.Errorf("%s: %w", op, domain.ErrStaleWrite)
}
