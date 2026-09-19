package presentationservice

// presentation_blocks.go — 内容模板内的全局块引用展开（core.globalref）。
//
// 内容模板是「完整文档层」，内部可以引用页眉/页脚/信任徽章等全局区块
// （docs/02-D §1.2：两者是包含关系，不是二选一）；构建期由本适配器把区块
// 文档内联进同一次编译输出，访问面仍是纯静态（无运行时拼接）。
//
// 与手工 Page 路径（page/service/page_assemble.go）同口径：
//   - 同一次编译内按块 ID 缓存（同一块被多次引用只查一次库）；
//   - 失败结果同样缓存，避免重复查库；
//   - reuse_mode=template 的块是「一次性复制」语义，不允许被引用展开，
//     命中即报错暴露（正常流程下副本已在插入时并入文档）。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	"go_wp/internal/pipeline"
)

// blockResolverAdapter 把 block 契约适配为 builder 的 core.BlockResolver。
type blockResolverAdapter struct {
	blocks blockcontract.BlockService
	ctx    context.Context
	// projectID 是块查询的必填 scope（block.Detail 用它做跨工程越权防护）：
	// 漏传只会得到「参数缺失」，而这里是降级路径 —— 构建照常完成、产物少一截。
	projectID string
	cache     map[string]*builder.Page
	errs      map[string]error
}

// newBlockResolverAdapter 构造单次编译的块解析适配器（缓存随编译实例存活）。
func newBlockResolverAdapter(blocks blockcontract.BlockService, ctx context.Context, projectID string) *blockResolverAdapter {
	return &blockResolverAdapter{
		blocks: blocks, ctx: ctx, projectID: projectID,
		cache: map[string]*builder.Page{}, errs: map[string]error{},
	}
}

// ResolveBlockRoot 实现 core.BlockResolver：按块 ID 返回块文档 root 节点。
func (a *blockResolverAdapter) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	page, err := a.blockPage(blockID)
	if err != nil {
		return nil, err
	}
	return page.Root, nil
}

// blockPage 解析块文档为 builder.Page（带缓存）。
func (a *blockResolverAdapter) blockPage(blockID string) (*builder.Page, error) {
	if page, ok := a.cache[blockID]; ok {
		return page, nil
	}
	if err, ok := a.errs[blockID]; ok {
		return nil, err
	}
	fail := func(err error) (*builder.Page, error) {
		a.errs[blockID] = err
		return nil, err
	}
	if a.blocks == nil {
		return fail(fmt.Errorf("全局块 %s 不可用（block 契约未装配）", blockID))
	}
	block, err := a.blocks.Detail(a.ctx, &blockcontract.DetailReq{ProjectID: a.projectID, ID: blockID})
	if err != nil || block == nil {
		return fail(fmt.Errorf("全局块 %s 不可用", blockID))
	}
	if block.ReuseMode == "template" {
		return fail(fmt.Errorf("全局块 %s 为一次性复制片段，不能被引用展开", blockID))
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		return fail(err)
	}
	a.cache[blockID] = page
	return page, nil
}

// 编译期断言：适配器实现 core.BlockResolver。
var _ core.BlockResolver = (*blockResolverAdapter)(nil)

// ResolveStructureDocument 实现 pipeline.StructureTemplatePort：结构槽位（页眉 / 页脚）
// 绑定的结构模板文档来源。
//
// 薄适配：版本解析与文档严格校验都在 contenttemplate 契约里
// （ResolveTemplateByIDScoped 按模板类型校验，并拒绝结构模板里的字段绑定），
// 这里只把「契约未装配 / 模板不存在 / 文档为空」统一成错误 —— 三者对构建期的含义
// 是同一个：这套模板不可用，回退到该槽位的块绑定（见 pipeline.BuildStructureSlots）。
func (s *Service) ResolveStructureDocument(ctx context.Context, projectID, templateID string) ([]byte, error) {
	if s == nil || s.templates == nil {
		return nil, fmt.Errorf("结构模板 %s 不可用（contenttemplate 契约未装配）", templateID)
	}
	tpl, err := s.templates.ResolveTemplateByIDScoped(ctx, projectID, templateID)
	if err != nil {
		return nil, err
	}
	if tpl == nil || len(tpl.Document) == 0 {
		return nil, fmt.Errorf("结构模板 %s 无可用文档", templateID)
	}
	return tpl.Document, nil
}

// 编译期断言：本服务是结构模板解析端口（结构槽位的模板来源）。
var _ pipeline.StructureTemplatePort = (*Service)(nil)

// ListBlockSourceRefs 列出文档树引用了该块的自动发布实例（审计 ARCH-02）。
//
// 逐工程扇出（DB-009 第二批）：块 id 说不出工程，而 presentation_instances 带 FORCE 策略 ——
// 漏作用域时这条查询静默返回空，删除保护会据此**放行**（正是本 finding 要拦住的形态）。
//
// Detail 区分 override_document 与 snapshot：两者的解除路径不同（改实例文档 vs
// 改模板后重建），合并成一条会让操作者以为改完一处就够。
func (s *Service) ListBlockSourceRefs(ctx context.Context, blockID string) (out []blockcontract.BlockUsage, err error) {
	if s == nil || s.project == nil {
		// 没有工程契约就枚举不出工程，而 instances 带 FORCE 策略：宁可显式失败，
		// 也不返回空集合 —— 空集合在删除保护里等于「没有引用」。
		return nil, errors.New("project 契约未装配，无法逐工程反查自动发布实例的块引用")
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range projects {
		projectID := strings.TrimSpace(projects[i].ID)
		if projectID == "" {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		rows, rerr := s.m.ListBlockDocumentRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for j := range rows {
			label := strings.TrimSpace(rows[j].URLPath)
			if label == "" {
				label = strings.TrimSpace(rows[j].EntityType) + ":" + strings.TrimSpace(rows[j].EntityID)
			}
			detail := "snapshot"
			if !rows[j].FromSnapshot {
				detail = "override_document"
			}
			out = append(out, blockcontract.BlockUsage{
				Kind: blockcontract.UsageKindPresentationInstance, ProjectID: projectID,
				EntityID: rows[j].InstanceID, Label: label, Detail: detail,
			})
		}
	}
	return out, nil
}
