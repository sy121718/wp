// Package blockservice 实现全局块业务用例。
//
// 本文件只放 Service 结构体、构造函数与装配自检；各能力域见 block_crud.go（增删改查）、
// block_ast.go（文档克隆与校验）、block_reference.go（引用检查与明细）、
// block_stale.go（失效传播）、block_normalize.go（字段归一化），
// 以及既有的 block_errors.go（错误集）与 block_scope.go（逐工程定位）。
package blockservice

import (
	"context"

	blockcontract "go_wp/internal/module/block/contract"
	blockmodel "go_wp/internal/module/block/model"
	projectcontract "go_wp/internal/module/project/contract"
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
