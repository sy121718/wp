package pubservice

import (
	"context"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"

	"gorm.io/gorm"
)

// ListActivePathsByPageTx 是 ListActivePaths 的事务内版本：路径集合从调用方
// 已经持锁的同一事务里重读，供删除/下线在锁内复核登记事实。
func (s *Service) ListActivePathsByPageTx(ctx context.Context, tx *gorm.DB, req *pubdto.ListActivePathsReq) (paths []string, err error) {
	if tx == nil {
		return nil, errTxRequired
	}
	if req == nil {
		return nil, pubenums.Err(pubenums.ErrInvalidParam)
	}
	return s.model.ListRoutePathsByPageTx(ctx, tx, req.ProjectID, req.PageID)
}
