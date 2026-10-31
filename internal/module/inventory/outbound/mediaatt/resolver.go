package mediaatt

import (
	"context"
	"errors"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	mediacontract "go_wp/internal/module/media/contract"
	mediadto "go_wp/internal/module/media/dto"
)

type Resolver struct {
	svc mediacontract.MediaService
}

var _ inventorycontract.AttachmentResolver = (*Resolver)(nil)

func New(svc mediacontract.MediaService) *Resolver { return &Resolver{svc: svc} }

func (r *Resolver) ResolveAttachment(ctx context.Context, projectID string, id int64) (*inventorycontract.AttachmentBrief, error) {
	one, err := r.resolveOne(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	if one == nil {
		return nil, errors.New(inventorycontract.ErrAttachmentNotFound)
	}
	return one, nil
}

func (r *Resolver) ResolveAttachments(ctx context.Context, projectID string, ids []int64) ([]*inventorycontract.AttachmentBrief, error) {
	out := make([]*inventorycontract.AttachmentBrief, 0, len(ids))
	for _, id := range ids {
		one, err := r.resolveOne(ctx, projectID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, nil
}

func (r *Resolver) resolveOne(ctx context.Context, projectID string, id int64) (*inventorycontract.AttachmentBrief, error) {
	if r.svc == nil || id <= 0 {
		return nil, nil
	}
	got, err := r.svc.Detail(ctx, &mediadto.DetailReq{ProjectID: projectID, ID: uint64(id)})
	if err != nil {

		if isBusinessNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if got == nil || got.ID == 0 {
		return nil, nil
	}
	return &inventorycontract.AttachmentBrief{
		ID:       int64(got.ID),
		FileName: got.FileName,
		FileSize: got.FileSize,
		MimeType: got.MimeType,
		URL:      got.URL,
	}, nil
}

func isBusinessNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, key := range []string{"erratachmentnotfound", "errmmedianotfound", "not found"} {
		if strings.Contains(msg, key) {
			return true
		}
	}
	return false
}
