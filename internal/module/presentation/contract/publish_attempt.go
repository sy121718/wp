package presentationcontract

import (
	"context"
	"time"
)

type PublishAttemptRow struct {
	ID int64

	Trigger string

	Outcome   string
	ErrorText string

	Langs []string

	Attempts int

	ArtifactVersion int64
	ArtifactHash    string

	OperatorID   int64
	OperatorName string
	DurationMs   int64

	CreatedAt time.Time
}

type PublishAttemptCursor struct {
	CreatedAt time.Time
	ID        int64
}

type PublishAttemptPage struct {
	Rows []PublishAttemptRow

	Total int64

	HasMore bool

	Next PublishAttemptCursor
}

type PublishAttemptPort interface {
	PublishAttempts(ctx context.Context, projectID, entityType, entityID string, limit int) (res PublishAttemptPage, err error)
}
