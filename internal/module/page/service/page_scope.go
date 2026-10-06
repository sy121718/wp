package pageservice

// page_scope.go — 跨工程入口的逐工程扇出（DB-009 第三批）。
//
// 一批 page 入口的签名里没有工程参数：契约由 workbench 等消费方编译期依赖（改签名会连带动
// 一大片），pipeline.DependencyTarget 也只带 (kind,key)。它们的语义本来就是跨工程的
// （整站标记待重建、按主题/块标记、全站草稿扫描），而 pages 在迁移 215 里带 FORCE 策略，
// 作用域只能落到某一个具体工程。
//
// 处理办法：枚举工程表后**逐工程独立作用域**执行（每个工程各自一次 set_config + 事务），
// 再合并结果。这不是「退化为不限工程」—— 每个事务的 app.project_id 都取确定值，
// 换非超级角色后每条语句都真的受策略约束。
//
// 为什么不合并成一次查询：RLS 的作用域是**单值**会话变量，把多个工程的 id 并进一次
// 查询只能靠放宽谓词，那等于取消隔离。presentation 的 MarkStaleByDependency（第二批）
// 是同一形状的样板。
//
// 工程表为空或读不到时**显式失败**：静默返回空结果会把「读不到工程表」伪装成
// 「没有受影响的页面」—— 那正是这一步要消灭的 fail-silent。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	pagemodel "go_wp/internal/module/page/model"
)

// fanoutProjectIDs 返回逐工程扇出要用的工程清单。
//
// 数量级很小（站点工程），逐个设一次作用域比在数据层引入 BYPASSRLS 连接便宜得多。
func (s *Service) fanoutProjectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	// 契约未注入就是装配漏接，直接失败。
	//
	// 这里原来回退到 `model.ListAllProjectIDs`（直接读 projects 表）：它能工作，但
	// **工程清单的所有权在 project 模块**，page 的 model 层只该碰本模块的表。更糟的是
	// 回退让漏接表现为「一切正常」，而 warn 日志没人看 —— 于是同一份「列出全部工程」
	// 的 SQL 在 page / order / block / navigation 里各存一份，四份将来会各自漂移
	// （比如某个模块开始按 create_time 排序、另一个按 id）。漏接时整站标记 / 全站扫描 /
	// 依赖扇出会整体失效，那本来就该是一次响亮的失败。
	if s.project == nil {
		return nil, ErrProjectRequired
	}
	list, err := s.project.List(ctx)
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
		// 一个工程都没有：不是「没有受影响页面」，而是没有可作用域的工程。
		return nil, ErrProjectRequired
	}
	return ids, nil
}

// locatePageInProjects 按页面 id 定位页面：逐工程独立作用域按 id 取，命中即返回。
//
// 页面 id 是主键（跨工程不会重复），所以逐工程探测的结果是确定的；反过来，
// 「不设作用域按 id 直查」在换非超级角色后是静默 ErrRecordNotFound ——
// 那种形态会让「页面明明在，却报不存在」。全部未命中返回 gorm.ErrRecordNotFound。
func (s *Service) locatePageInProjects(ctx context.Context, id string) (*pagemodel.PageEntity, error) {
	ids, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	var lastErr error = gorm.ErrRecordNotFound
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		page, gerr := s.model.GetByID(ctx, id, projectID)
		if gerr == nil {
			return page, nil
		}
		lastErr = gerr
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, lastErr
}
