package pagecontract

import "context"

type PublishedPageLocator interface {
	PublishedPagePath(ctx context.Context, projectID, pageID, lang string) (string, error)
}
