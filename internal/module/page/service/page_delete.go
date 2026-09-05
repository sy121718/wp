package pageservice

import (
	"context"
	"strings"
	"time"

	pagedto "go_wp/internal/module/page/dto"
)

// Delete 软删页面：deleted_at 置时间（审计留痕，行保留），并原子释放该页面
// 全部路径占用（reserved/active/redirect）。释放后同路径可被新页面重新创建，
// 解决「软删后 routeCount==1 残留、路径永久占用」的能力缺口。
// nil/空 ID 属于请求不合法（ErrInvalidParam）；页面不存在或已软删统一返回 ErrPageNotFound。
func (s *Service) Delete(ctx context.Context, req *pagedto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrInvalidParam
	}
	if err = s.model.SoftDeleteWithRoutes(ctx, req.ID, time.Now().UTC()); err != nil {
		return mapPersistenceError(err)
	}
	return nil
}
