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

// listEnabledMenusCached 带短 TTL 的「启用菜单」缓存（PERF-010）。
// 菜单变更频率低，权限路由构建却每次请求都触发；缓存 30s 可显著减少 DB 读。
//
// 三点关于「在哪一层过滤」的取舍，改这里之前先读：
//
//  1. status=1 已下推到 SQL（ListEnabled）：它与用户无关，禁用项从不进树；
//  2. type 不下推：同一份缓存要同时服务导航树（只要 type=1/2）与动态路由
//     （还要 type=3 的按钮权限点算按钮授权），砍掉 type 就得缓存两份；
//  3. permission_code 与用户权限码的交集留在内存：它因人而异，下推会让这条 SQL
//     的文本随用户变化（无法复用执行计划），缓存键也得带上用户权限集合 ——
//     等于放弃跨用户共享这份缓存，每次请求都回落到查库。
//     菜单表当前百来行，一次全表读 + 内存过滤比按用户查更划算。
func (s *Service) listEnabledMenusCached(ctx context.Context) ([]adminmodel.MenuEntity, error) {
	menuCacheMu.RLock()
	if len(menuCache) > 0 && time.Since(menuCacheAt) < menuCacheTTL {
		cached := append([]adminmodel.MenuEntity(nil), menuCache...)
		menuCacheMu.RUnlock()
		return cached, nil
	}
	menuCacheMu.RUnlock()

	all, err := s.mm.ListEnabled(ctx)
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
