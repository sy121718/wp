package presentationcontract

import (
	"context"
	"time"
)

type PublishRecordRow struct {
	InstanceID string
	EntityType string
	EntityID   string

	InstanceRole string

	URLPath string

	Lang string

	ActivePath string

	ArtifactVersion int64
	ArtifactHash    string

	PublishedAt time.Time

	Active bool

	Stale bool
}

type PublishRecordPort interface {
	PublishRecords(ctx context.Context, projectID, entityType string, entityIDs []string) ([]PublishRecordRow, error)
}
