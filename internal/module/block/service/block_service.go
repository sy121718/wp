package blockservice

// Package blockservice 实现全局块业务用例。
//
// 本文件只放 Service 结构体、构造函数与装配自检；各能力域见 block_crud.go（增删改查）、
// 以及既有的 block_errors.go（错误集）与 block_scope.go（逐工程定位）。

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

// 传播器由编排层注入；未注入时只记日志，不阻断块本身的写入。

// 块被页面/其他块引用时禁止删除，依赖由编排层注入的回调反查（block 不反向依赖 page）。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/block/model"
	"go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

var _ blockcontract.BlockService = (*Service)(nil)

// Service 全局块业务服务：跨页面复用的结构片段（页眉/页脚/区块）。
type Service struct {
	model    *blockmodel.Model
	projects projectcontract.ProjectService
	// propagate 全局块内容变更/删除后的 stale 传播器（编排层注入，可空）。
	// block 不直接依赖 page 模块，传播由注入的回调完成，避免 block↔page 装配循环。
	propagate func(ctx context.Context, blockID string) error
	// referenced 引用检查器（编排层注入，可空）：global 块删除 / global→template 切换前
	// 判断是否仍被源码引用。未注入时不拦截（兼容直连装配）。
	//
	// 与 usages 二选一：它只回答「有没有」，说不清「是哪一类、哪几个实体」——
	// 生产装配用 usages，本字段保留给只关心布尔结果的装配与单测。
	referenced func(ctx context.Context, blockID string) (bool, error)
	// usages 引用明细检查器（编排层注入，可空）：返回引用该块的全部源码引用。
	//
	// 为什么要明细：块引用散在文档 JSONB 的任意深度（页面正文 / 页眉页脚 / 槽位 /
	// 其它块 / 模板 / 实例快照），只说「被引用」等于让操作者自己把整站翻一遍。
	usages func(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)
}

// NewService 创建全局块服务。
func NewService(model *blockmodel.Model, projects projectcontract.ProjectService) *Service {
	return &Service{model: model, projects: projects}
}

// RequireWiring 编排完成后调用：传播器或引用检查器未注入则 fail-fast。
func (s *Service) RequireWiring() {
	if s.propagate == nil || (s.referenced == nil && s.usages == nil) {
		panic("block.Service: stale 传播器与引用检查器必须注入（装配层在 page 装配后调用 RequireWiring）")
	}
}

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

// 本包 sentinel error：重导出 contract 包的同一实例（审计项「block 错误码靠中文文案
// strings.Contains 匹配」）。
//
// sentinel 定义在 contract 包（错误是跨模块契约的一部分，调用方按 errors.Is 精确分类
// 而无需 import 本 service 包）；本包重导出同一实例，保证 service 内部返回的 error 与
// 调用方 errors.Is 判等一致。Error() 文案与 blockenums 常量一致，前端响应文案不变。
var (
	// ErrParamRequired 请求参数缺失（nil 请求、空/空白 ID、空 projectID 等）。
	ErrParamRequired = blockcontract.ErrParamRequired
	// ErrNotFound 全局块不存在。
	ErrNotFound = blockcontract.ErrNotFound
	// ErrProjectNotFound 工程不存在。
	ErrProjectNotFound = blockcontract.ErrProjectNotFound
	// ErrProjectRequired 缺可作用域的工程（DB-009）：只带 id 的入口逐工程定位时工程清单为空。
	ErrProjectRequired = blockcontract.ErrProjectRequired
	// ErrNameRequired 块名称缺失。
	ErrNameRequired = blockcontract.ErrNameRequired
	// ErrInvalidDoc 块文档不合法（与页面文档同构校验失败）。
	ErrInvalidDoc = blockcontract.ErrInvalidDoc
	// ErrInvalidKind 块类型不合法（非 block/header/footer）。
	ErrInvalidKind = blockcontract.ErrInvalidKind
	// ErrInvalidCategory 块分类不合法（未通过白名单）。
	ErrInvalidCategory = blockcontract.ErrInvalidCategory
	// ErrDuplicate 同工程同名块已存在。
	ErrDuplicate = blockcontract.ErrDuplicate
	// ErrInvalidReuseMode 块复用方式不合法（非 global/template）。
	ErrInvalidReuseMode = blockcontract.ErrInvalidReuseMode
	// ErrBlockInUse global 块仍被页面/主题引用：删除或切换 global→template 前须先解除引用或 Force。
	ErrBlockInUse = blockcontract.ErrBlockInUse
)

// SetStalePropagator 注入全局块 stale 传播器（编排层在 page 装配后绑定，
// 打破「block 先于 page 装配」的顺序约束）。未注入时内容变更不传播。
func (s *Service) SetStalePropagator(p func(ctx context.Context, blockID string) error) {
	s.propagate = p
}

// propagateStale 块内容变更/删除后触发引用方 stale 传播。
// reuse_mode=template 的块不传播（docs/02-D §9）：插入时已复制 AST，页面持有独立副本，
// 源块修改不影响任何页面。传播器未注入或传播失败只记日志，不阻断保存/删除主流程。
func (s *Service) propagateStale(ctx context.Context, blockID, reuseMode string) {
	if reuseMode == blockmodel.ReuseTemplate {
		return // 一次性复制的片段不传播 stale
	}
	if s.propagate == nil {
		logger.Scene("block").With("block_id", blockID).Error(
			errors.New("stale 传播器未注入"),
			"全局块变更未传播 stale，请检查 dashboard 装配是否调用 RequireWiring",
		)
		return
	}
	if err := s.propagate(ctx, blockID); err != nil {
		logger.Scene("block").With("block_id", blockID).Error(err, "全局块 stale 传播失败")
	}
}

// SetReferenceChecker 注入引用检查器（**布尔形式**：只回答「有没有引用」）。
//
// 与 SetStalePropagator 同模式（打破「block 先于 page 装配」的顺序约束）。
// 生产装配用的是 SetReferenceUsageChecker（明细形式，两处都注入时以它为准）；
// 本 setter 当前的调用方是**测试** —— 用例用它注入一个固定的引用判定，
// 或用 SetReferenceChecker(nil) 撤掉默认检查器让用例独立跑。
// 生产新增调用方之前先确认：明细检查器为什么不够。
func (s *Service) SetReferenceChecker(r func(ctx context.Context, blockID string) (bool, error)) {
	s.referenced = r
}

// SetReferenceUsageChecker 注入引用明细检查器（生产装配用；见 SetReferenceChecker）。
//
// 与 SetReferenceChecker 的关系：两处都注入时以本检查器为准（它能回答「哪些实体」，
// 布尔检查器只能回答「有没有」）—— 不做「两个都查、取或」是因为那会把两套口径
// 拼在一起，一处漏改就变成「明明被引用却放行」。
func (s *Service) SetReferenceUsageChecker(r func(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)) {
	s.usages = r
}

// blockRefState 取「仍被哪些源码引用」：明细检查器优先，退回布尔检查器。
//
// 两个方向的失败都朝**拒绝**走（删除不可逆，宁拒勿删）：
//   - 检查器报错：按被引用处理（拿不到引用清单不能当成没有引用）；
//   - 检查器未注入：同样视为被引用。
//
// 返回的 usages 可能为空而 referenced 为 true（布尔检查器命中 / 检查失败）：
// 那种情况下拒绝依然成立，只是提示退回到通用文案。
func (s *Service) blockRefState(ctx context.Context, blockID string) (usages []blockcontract.BlockUsage, referenced bool) {
	if s.usages != nil {
		list, err := s.usages(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "块引用明细检查失败，按被引用处理")
			return nil, true
		}
		return list, len(list) > 0
	}
	if s.referenced != nil {
		ok, err := s.referenced(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "块引用检查失败，按被引用处理")
			return nil, true
		}
		return nil, ok
	}
	return nil, true
}

// ListBlockSourceRefs 列出引用该块的**其它全局块**（审计 ARCH-02：嵌套块引用）。
//
// 逐工程扇出（DB-009）：块 id 说不出工程，而 blocks 带 FORCE 策略 —— 漏作用域时
// 这条查询静默返回空，删除保护会据此放行。
func (s *Service) ListBlockSourceRefs(ctx context.Context, blockID string) (out []blockcontract.BlockUsage, err error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		rows, rerr := s.model.ListBlockDocumentRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for i := range rows {
			out = append(out, blockcontract.BlockUsage{
				Kind: blockcontract.UsageKindBlockDocument, ProjectID: projectID,
				EntityID: rows[i].ID, Label: rows[i].Name,
			})
		}
	}
	return out, nil
}
