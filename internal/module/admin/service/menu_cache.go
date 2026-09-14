package adminservice

import (
	"context"
	"sync"
	"time"

	adminmodel "go_wp/internal/module/admin/model"
)

const menuCacheTTL = 30 * time.Second

var (
	menuCacheMu sync.RWMutex
	menuCache   []adminmodel.MenuEntity
	menuCacheAt time.Time
)

// listAllMenusCached 带短 TTL 的菜单全表缓存（PERF-010）。
// 菜单变更频率低，权限路由构建却每次请求都触发；缓存 30s 可显著减少 DB 读。
func (s *Service) listAllMenusCached(ctx context.Context) ([]adminmodel.MenuEntity, error) {
	menuCacheMu.RLock()
	if len(menuCache) > 0 && time.Since(menuCacheAt) < menuCacheTTL {
		cached := append([]adminmodel.MenuEntity(nil), menuCache...)
		menuCacheMu.RUnlock()
		return cached, nil
	}
	menuCacheMu.RUnlock()

	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	menuCacheMu.Lock()
	menuCache = append([]adminmodel.MenuEntity(nil), all...)
	menuCacheAt = time.Now()
	menuCacheMu.Unlock()
	return all, nil
}

// invalidateMenuCache 菜单写操作后清缓存。
func invalidateMenuCache() {
	menuCacheMu.Lock()
	menuCache = nil
	menuCacheAt = time.Time{}
	menuCacheMu.Unlock()
}
