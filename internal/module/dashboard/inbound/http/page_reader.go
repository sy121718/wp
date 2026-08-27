// Package dashboardhttp 页面模块对外依赖契约（本包内声明，避免跨模块导入 service/model）。
package dashboardhttp

import (
	"context"

	pagedto "go_wp/internal/module/page/dto"
)

// PageReader dashboard 需要的 page 模块只读能力（Detail 的最小子集）。
type PageReader interface {
	Detail(ctx context.Context, req *pagedto.DetailReq) (res *pagedto.PageResp, err error)
}
