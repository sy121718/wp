package projectservice

// structure_template_options.go — 主题设置页「选结构模板」下拉的候选读取。
//
// 数据源是注入的 StructureTemplateOptionsPort（装配层用 contenttemplate 契约的只读 List
// 实现）：project 模块不认识 content_templates 表，也不 import contenttemplate 的
// service/model —— 端口留在消费者侧，与本模块的 LocaleRetirePort 同一形状。

import (
	"context"
	"strings"

	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

// StructureTemplateOptions 列出本工程可绑定的结构模板（页眉 / 页脚）。
//
// 未注入端口时返回空列表：调用方（主题设置页）会渲染成只有「不绑定」一项的下拉。
// 这里不返回错误 —— 下拉缺候选是配置面变窄，把它变成一次保存失败是更坏的选择；
// 能力缺失由 Warn 日志与页面数据（候选为空）共同表达。
func (s *Service) StructureTemplateOptions(ctx context.Context, projectID string) (
	opts []projectcontract.StructureTemplateOption, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, nil
	}
	if s.structureTemplateOpts == nil {
		logger.Scene("theme").Warn("结构模板候选端口未装配：主题设置页的「选结构模板」下拉只有「不绑定」")
		return nil, nil
	}
	return s.structureTemplateOpts.ListStructureTemplateOptions(ctx, projectID)
}
