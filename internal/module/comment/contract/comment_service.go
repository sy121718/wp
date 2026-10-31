package commentcontract

import (
	"context"

	commentdto "go_wp/internal/module/comment/dto"
)

type LabelPair struct {
	Key      string
	Fallback string
}

type EntityType struct {
	Type string

	Label LabelPair
}

type EntityPolicy interface {
	AllowComment(ctx context.Context, req *PolicyReq) (denial *PolicyDenial, err error)
}

type PolicyReq struct {
	ProjectID  string
	EntityType string
	EntityID   string

	UserID uint64
}

type PolicyDenial struct {
	Message string

	Fallback string
}

type FragmentPort interface {
	ListApproved(ctx context.Context, req *commentdto.ListReq) (res *commentdto.ListResp, err error)

	Submit(ctx context.Context, req *commentdto.SubmitReq) (res *commentdto.SubmitResp, err error)
}

type CommentService interface {
	FragmentPort

	AdminList(ctx context.Context, req *commentdto.AdminListReq) (res *commentdto.AdminListResp, err error)

	Review(ctx context.Context, req *commentdto.ReviewReq) (res *commentdto.ReviewResp, err error)

	EntityTypeLabels(tr func(key, fallback string) string) []commentdto.EntityType

	IsRegisteredEntityType(entityType string) bool
}
