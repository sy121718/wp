// Package projectcontract 定义 project 模块对外契约。
package projectcontract

import (
	"context"

	projectdto "go_wp/internal/module/project/dto"
)

// ProjectService 站点工程与 SiteSettings 业务能力。
type ProjectService interface {
	Create(ctx context.Context, req *projectdto.CreateReq) (res *projectdto.ProjectResp, err error)
	Detail(ctx context.Context, req *projectdto.DetailReq) (res *projectdto.ProjectResp, err error)
	Update(ctx context.Context, req *projectdto.UpdateReq) (res *projectdto.ProjectResp, err error)
	Exists(ctx context.Context, id string) (exists bool, err error)
}
