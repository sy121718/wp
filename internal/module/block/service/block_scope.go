package blockservice

// block_scope.go — 只带 id 的入口如何拿到工程作用域（审计 DB-009）。
//
// blocks 在迁移 215 里带 FORCE 策略：不带 app.project_id 的读写在非超级角色下
// **静默落空**（读到 0 行 → ErrRecordNotFound、改 0 行、删 0 行且不报错），
// 而这三个入口的请求里只有块 id —— 后台块编辑器的「更新 / 删除 / 复制 AST」都属此类
// （Update / Delete / CloneAST，它们共用 getExistingBlock 这一跳定位）。
//
// 做法与 page / order / navigation 侧同形：**先逐工程独立作用域探测出归属**（块 id 是
// 主键，跨工程不会重复命中），拿到实体自带的 project_id 之后再进事务 —— 事务内的作用域
// 用它，绝不退回「不限工程」。方向也是安全的：探测本身受策略约束，拿不到别的工程的行。
//
// 为什么探测放在事务**外**：rls.InProjectScope 会新开事务、另取连接，放进已开的事务里
// 会让外层未提交的数据不可见、同表写入还可能自锁（见 pkg/rls.ScopeTx 的说明）。
// 工程归属是稳定属性（全局块不会换工程），所以事务外取到的作用域在事务内依然成立。
//
// 工程清单为空或读不到时**显式失败**：静默返回空清单会把「读不到工程表」伪装成
// 「块不存在」—— 那正是本批要消灭的 fail-silent。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	blockmodel "go_wp/internal/module/block/model"
)

// projectIDs 定位用的工程清单。
//
// 数量级很小（站点工程），逐个设一次作用域比在数据层引入 BYPASSRLS 连接便宜得多。
func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	// 契约未注入就是装配漏接，直接失败。
	//
	// 这里原来回退到 `model.ListAllProjectIDs`（直接读 projects 表）。它能工作，但
	// **工程清单的所有权在 project 模块**，block 的 model 层只该碰本模块的表；回退还会
	// 让漏接表现为「一切正常」，于是同一份「列出全部工程」的 SQL 在 block / page /
	// order / navigation 里各存一份，四份将来会各自漂移。
	if s.projects == nil {
		return nil, ErrProjectRequired
	}
	list, err := s.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		if id := strings.TrimSpace(list[i].ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		// 一个工程都没有：不是「块不存在」，而是没有可作用域的工程。
		return nil, ErrProjectRequired
	}
	return ids, nil
}

// locateBlockEntity 按块 id 定位实体：逐工程独立作用域按 id 取，命中即返回。
//
// 块 id 是主键（跨工程不会重复），所以逐工程探测的结果是确定的；反过来，
// 「不设作用域按 id 直查」在换非超级角色后是静默的 ErrRecordNotFound ——
// 那种形态会让「块明明在，却报不存在」。全部未命中返回 ErrNotFound。
func (s *Service) locateBlockEntity(ctx context.Context, id string) (*blockmodel.BlockEntity, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.model.GetByID(ctx, id, projectID)
		if gerr == nil {
			return e, nil
		}
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, ErrNotFound
}
