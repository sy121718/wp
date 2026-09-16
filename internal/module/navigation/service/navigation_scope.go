package navigationservice

// navigation_scope.go — 只带 id 的入口如何拿到工程作用域（审计 DB-009）。
//
// navigations 在迁移 215 里带 FORCE 策略：不带 app.project_id 的读写在非超级角色下
// **静默落空**（读到 0 行、改 0 行、删 0 行且不报错），而这三个入口的请求里只有 id ——
// 后台导航管理页的「更新 / 详情 / 删除」都属此类（Update / Get / Delete）。
//
// 做法与 page / order 侧同形：**先逐工程独立作用域探测出归属**（id 是主键，跨工程不会
// 重复命中），拿到实体自带的 project_id 之后再进事务 —— 事务内的作用域用它，
// 绝不退回「不限工程」。方向也是安全的：探测本身受策略约束，拿不到别的工程的行。
//
// 为什么探测放在事务**外**：rls.InProjectScope 会新开事务、另取连接，放进已开的事务里
// 会让外层未提交的数据不可见、同表写入还可能自锁（见 pkg/rls.ScopeTx 的说明）。
// 工程归属是稳定属性（导航项不会换工程），所以事务外取到的作用域在事务内依然成立。
//
// 工程清单为空或读不到时**显式失败**：静默返回空清单会把「读不到工程表」伪装成
// 「导航项不存在」—— 那正是本批要消灭的 fail-silent。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	"go_wp/pkg/logger"
)

// projectIDs 定位用的工程清单。
//
// 数量级很小（站点工程），逐个设一次作用域比在数据层引入 BYPASSRLS 连接便宜得多。
func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.m == nil {
		return nil, errors.New(navigationenums.ErrProjectRequired)
	}
	var ids []string
	if s.projects != nil {
		list, err := s.projects.List(ctx)
		if err != nil {
			return nil, err
		}
		ids = make([]string, 0, len(list))
		for i := range list {
			if id := strings.TrimSpace(list[i].ID); id != "" {
				ids = append(ids, id)
			}
		}
	} else {
		// 契约未注入的兜底：漏接一处装配就会让「按 id 更新 / 查询 / 删除」整体静默失效
		//（表现为「导航项明明在却报不存在」），那正是本批要消灭的失败形态。
		// 兜底路径记一条 warn：装配缺失应当被看见，而不是靠运气正常工作。
		logger.Scene("navigation").Warn("project 契约未注入，逐工程定位回退到 projects 表清单（请检查装配点）")
		list, err := s.m.ListAllProjectIDs(ctx)
		if err != nil {
			return nil, err
		}
		ids = list
	}
	if len(ids) == 0 {
		// 一个工程都没有：不是「导航项不存在」，而是没有可作用域的工程。
		return nil, errors.New(navigationenums.ErrProjectRequired)
	}
	return ids, nil
}

// locateNavigation 按导航项 id 定位实体：逐工程独立作用域按 id 取，命中即返回。
//
// id 是主键（跨工程不会重复），所以逐工程探测的结果是确定的；反过来，
// 「不设作用域按 id 直查」在换非超级角色后是静默的 ErrRecordNotFound ——
// 那种形态会让「导航项明明在，却报不存在」。全部未命中返回 gorm.ErrRecordNotFound，
// 由调用方映射成模块自己的 ErrNotFound。
func (s *Service) locateNavigation(ctx context.Context, id string) (*navigationmodel.NavigationEntity, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.m.Get(ctx, projectID, id)
		if gerr == nil {
			return e, nil
		}
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, gorm.ErrRecordNotFound
}
