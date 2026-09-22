package command

import (
	"context"
	"encoding/json"
	"time"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ProviderRunRepository interface {
	CreateProviderRun(context.Context, domain.ProviderRun) error
}

func providerRunForOperation(operation domain.Operation) (domain.ProviderRun, error) {
	return domain.NewProviderRun(operation.ID, operation.WorkspaceID, operation.ID, operation.CreatedBy, "extraction", operation.Provider, operation.Method, operation.TemplateVersion, operation.Status.String(), operation.InputBytes, operation.OutputBytes, operation.DurationMS, operation.TimedOut, operation.Error, operation.CreatedAt, operation.CompletedAt)
}

type providerRunTimer struct {
	started   time.Time
	inputSize int64
}

func startProviderRun(input any) providerRunTimer {
	raw, err := json.Marshal(input)
	if err != nil {
		return providerRunTimer{started: time.Now()}
	}
	return providerRunTimer{started: time.Now(), inputSize: int64(len(raw))}
}

func (t providerRunTimer) finish(resultID, workspace, actor id.ID, kind, provider, method, templateVersion, status, output, failure string, timedOut bool, createdAt time.Time) (domain.ProviderRun, error) {
	duration := time.Since(t.started).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	return domain.NewProviderRun(resultID, workspace, resultID, actor, kind, provider, method, templateVersion, status, t.inputSize, int64(len([]byte(output))), duration, timedOut, failure, createdAt, createdAt.Add(time.Duration(duration)*time.Millisecond))
}
