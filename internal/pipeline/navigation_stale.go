package pipeline

// navigation_stale.go — 导航失效派发端口 → Fanout 的适配器（审计遗留缺口）。
//
// 为什么适配器在这里而不是 navigation 模块：依赖键的构造（MenuKey）与依赖扇出都属发布
// 内核，navigation 只需表达「这个工程的这个菜单位置变了」。依赖方向由此保持单向：
// 装配层同时认识两边，navigation 不 import pipeline，pipeline 也不认识 navigation 的
// 表结构（只经契约）。

import (
	"context"
	"errors"

	navigationcontract "go_wp/internal/module/navigation/contract"
)

// MenuStaleAdapter 把 navigation 的失效派发端口接到依赖扇出上。
type MenuStaleAdapter struct {
	Fanout *Fanout
}

// NewMenuStaleAdapter 构造导航失效适配器（装配期注入 navigation 服务）。
func NewMenuStaleAdapter(f *Fanout) *MenuStaleAdapter {
	return &MenuStaleAdapter{Fanout: f}
}

// InvalidateMenu 实现 navigationcontract.MenuStaleDispatcher：
// 键 menu:{projectID}:{kind} 经扇出反查 page 与 presentation 两侧的依赖表并标记 stale。
//
// 永不因「没命中任何产物」报错：那个位置的菜单可能还没被任何页面绑定过（或都已重建），
// 这不是失败。适配器未接扇出是装配缺陷，显式返回错误由调用方记日志。
func (a *MenuStaleAdapter) InvalidateMenu(ctx context.Context, projectID, kind string) error {
	if a == nil || a.Fanout == nil {
		return errors.New("导航失效适配器未接入依赖扇出（装配缺陷）")
	}
	a.Fanout.InvalidateKeys(ctx, MenuKey(projectID, kind))
	return nil
}

// 编译期断言：装配层据此注入 navigation 服务。
var _ navigationcontract.MenuStaleDispatcher = (*MenuStaleAdapter)(nil)
