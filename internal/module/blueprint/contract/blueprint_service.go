package blueprintcontract

import (
	"context"
	json "go_wp/pkg/jsonx"

	blueprintdto "go_wp/internal/module/blueprint/dto"
)

type BlueprintService interface {
	Create(ctx context.Context, req *blueprintdto.CreateReq) (res *blueprintdto.BlueprintResp, err error)

	Update(ctx context.Context, req *blueprintdto.UpdateReq) (res *blueprintdto.BlueprintResp, err error)

	Publish(ctx context.Context, req *blueprintdto.PublishReq) (res *blueprintdto.BlueprintResp, err error)

	Get(ctx context.Context, req *blueprintdto.GetReq) (res *blueprintdto.BlueprintResp, err error)

	List(ctx context.Context, req *blueprintdto.ListReq) (list []*blueprintdto.BlueprintResp, err error)

	Delete(ctx context.Context, req *blueprintdto.DeleteReq) (err error)

	InitPageDocument(ctx context.Context, blueprintID string) (doc json.RawMessage, err error)
}
