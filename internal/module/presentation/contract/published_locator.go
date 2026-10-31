package presentationcontract

import "context"

type PublishedInstanceLocator interface {
	PublishedInstancePath(ctx context.Context, projectID, instanceID, lang string) (string, error)
}

type PublishedEntityLocator interface {
	PublishedEntityPaths(ctx context.Context, projectID, entityType, lang string, entityIDs []string) (map[string]string, error)
}

type PublishedEntityIndex interface {
	PublishedEntityIndex(context.Context, string, string, string) (map[string]string, error)
}
