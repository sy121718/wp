package inventorycontract

import "context"

type AttachmentResolver interface {
	ResolveAttachment(ctx context.Context, projectID string, id int64) (*AttachmentBrief, error)

	ResolveAttachments(ctx context.Context, projectID string, ids []int64) ([]*AttachmentBrief, error)
}

type AttachmentBrief struct {
	ID       int64
	FileName string
	FileSize int64
	MimeType string
	URL      string
}

const ErrAttachmentNotFound = "ErrAttachmentNotFound"
