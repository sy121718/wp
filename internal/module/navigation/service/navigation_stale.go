package navigationservice

// navigation_stale.go — 导航变更 → 依赖失效派发（审计遗留缺口）。
//
// 背景：pipeline.DepKindMenu 常量一直存在，但全仓没有任何发射点、构建期也没登记这条
// 依赖，于是「改公开站点导航（navigations 表）」之后，凡是把该菜单位置烘进产物的页面
// （page）与自动发布实例（presentation）都会永远停在旧字节 —— 导航在页眉/页脚，
// 全站可见，且全程没有任何报错。严重性高于块/内容那一类：它们的失效面是局部，
// 导航的失效面是整站。
//
// 两半配套（缺一不可）：
//   登记：构建期 core.nav 消费菜单位置时记入产物依赖（pipeline.CompileUsage.UseMenu
//         → page / presentation 的依赖提供者写 page_dependencies / presentation_dependencies）；
//   派发：本文件在导航写操作成功后把「哪个工程哪个位置变了」交给 pipeline.Fanout，
//         由它按依赖表反查受影响的来源并标记 stale（+ 自动重建）。

import (
	"context"
	"errors"
	"strings"

	navigationcontract "go_wp/internal/module/navigation/contract"
	"go_wp/pkg/logger"
)

// SetMenuStaleDispatcher 注入导航失效派发端口（装配期调用一次，之后只读）。
//
// 未注入时不 panic、只在本服务的写路径上记 error 日志：导航 CRUD 是最常用的后台操作，
// 让它整体不可用换取一条装配期错误不划算；装配缺陷由 routes 的 wiring 自检在启动时
// 直接拦下（见 internal/routers/wiring.go 的 navigation.SetMenuStaleDispatcher）。
func (s *Service) SetMenuStaleDispatcher(d navigationcontract.MenuStaleDispatcher) {
	if s == nil {
		return
	}
	s.staleMenu = d
}

// invalidateMenu 把一次导航写操作的结果派发给依赖扇出。
//
// 时机：**持久化写入之后**。当前三个写入口（Create / Update / Delete）各自只有一次
// 写语句（gorm 自动提交），所以「写成功后调用」就是「提交后调用」。若将来在同一个
// 入口里再加一处持久化写入（按硬规则必须包进同一个事务），这段派发必须留在事务**外面** ——
// 在事务内派发的话，事务一旦回滚，标记已经落库：那是一份没有任何对应内容变更的 stale，
// 表现为受影响页面被反复重建，且查不出是谁改的。
//
// 失败只记日志、不影响导航写入：失效派发是内容写入的后置副作用（与 content 扇出
// 同一口径），让已经成功的导航保存返回失败只会把用户导向「重试」，而重试并不能修好扇出。
func (s *Service) invalidateMenu(ctx context.Context, projectID, kind string) {
	projectID = strings.TrimSpace(projectID)
	kind = strings.TrimSpace(kind)
	if projectID == "" || kind == "" {
		// 键的两个分量缺一就构造不出 menu:{projectID}:{kind}（pipeline.MenuKey），
		// 派发只会打出一批误命中的 key。静默返回：这属于上面的调用方写错了参数。
		return
	}
	if s.staleMenu == nil {
		logger.Scene("navigation").With("project_id", projectID).With("kind", kind).
			Error(errors.New("导航失效派发端口未注入"),
				"导航变更未派发 stale，已发布页面/实例会停在旧导航上；请检查装配（wiring: navigation.SetMenuStaleDispatcher）")
		return
	}
	if err := s.staleMenu.InvalidateMenu(ctx, projectID, kind); err != nil {
		logger.Scene("navigation").With("project_id", projectID).With("kind", kind).
			Error(err, "导航变更后的失效派发失败（产物保持原状，需手动重建）")
	}
}

// invalidateNavigation 把一次「具体菜单项」写操作的结果派发给依赖扇出。
//
// 与 invalidateMenu 并列：按项引用的依赖键是 navigation:{itemID}（pipeline.NavigationKey），
// 只有本方法能命中。失败同样只记日志 —— 与导航位置派发同一口径（内容已写入，
// 标记失败只影响「下次构建会不会主动带上」）。
func (s *Service) invalidateNavigation(ctx context.Context, projectID, navigationID string) {
	projectID = strings.TrimSpace(projectID)
	navigationID = strings.TrimSpace(navigationID)
	if projectID == "" || navigationID == "" {
		return
	}
	if s.staleMenu == nil {
		logger.Scene("navigation").With("project_id", projectID).With("navigation_id", navigationID).
			Error(errors.New("导航失效派发端口未注入"),
				"菜单项变更未派发 stale，按项引用它的页面/实例会停在旧菜单上；请检查装配（wiring: navigation.SetMenuStaleDispatcher）")
		return
	}
	if err := s.staleMenu.InvalidateNavigation(ctx, projectID, navigationID); err != nil {
		logger.Scene("navigation").With("project_id", projectID).With("navigation_id", navigationID).
			Error(err, "菜单项变更后的失效派发失败（产物保持原状，需手动重建）")
	}
}

// InvalidateMenuLabels 按位置逐个派发失效（导航译文工作台写 sys_translation 后用）。
//
// 为什么走这条而不是自己拿实例契约去标：菜单标签的译文与菜单项本身属于**同一个**
// 依赖键（menu:{projectID}:{kind}，构建期 core.nav 消费位置时登记）——
// 译文写入与导航项写入必须打同一个键，否则「改了菜单文字」与「改了菜单项」两条路径
// 会各自维护一套口径，迟早分叉（分叉的表现是页面重建了一次，实例没有）。
// 键的构造与扇出都在发布内核（pipeline.MenuKey / MenuStaleAdapter），这里只转发。
//
// 时机同样是**持久化写入之后**（译文 Upsert 已经落库），失败只记日志 ——
// 与 invalidateMenu 同一口径：译文本身已写好，标记失败只影响「下次构建会不会主动带上」。
func (s *Service) InvalidateMenuLabels(ctx context.Context, projectID string, kinds []string) {
	for _, kind := range kinds {
		if ctx.Err() != nil {
			return
		}
		s.invalidateMenu(ctx, projectID, kind)
	}
}
