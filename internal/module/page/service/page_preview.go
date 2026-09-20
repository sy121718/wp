package pageservice

// page_preview.go — 预览编译用例：基于未落盘文档 JSON 编译完整 HTML。
// workbench 工作台预览（Preview/PreviewDraft/BlockPreview）复用本方法，
// 与正式构建共用 compileDocument 装配管线（docs/03-A §4.2 隔离预览）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go_wp/internal/builder"
	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/pkg/logger"
)

// CompilePreview 基于未落盘文档 JSON 编译完整 HTML（预览专用：不落盘、不影响产物）。
// 复用与正式构建同源的编译管线（compileDocument），仅错误语义按预览需求分类：
//   - 文档解析失败（JSON 非法或空文档）→ ErrPreviewInvalidDocument；
//   - 编译失败 → ErrPreviewCompileFailed；
//   - 组件模板加载/文档渲染失败 → 原样透传（调用方映射为内部错误）。
//
// projectID 为页面所属站点工程（调用方从页面记录取；块预览传块所属工程），
// currentPath 为页面逻辑访问路径（导航当前项高亮；块预览传空），
// lang 为预览目标语言（空 = 站点默认语言；工作台按 ?lang= 切换预览语言），
// 与正式构建一致地驱动导航等站点级资源解析——画布所见即产物。
func (s *Service) CompilePreview(ctx context.Context, docJSON []byte, projectID, currentPath, lang string) (html []byte, err error) {
	var page *builder.Page
	if err = json.Unmarshal(docJSON, &page); err != nil || page == nil {
		if err == nil {
			err = errors.New("草稿文档为空")
		}
		logger.Scene("build").With("err", err).Warn("预览文档解析失败")
		return nil, fmt.Errorf("%w: %v", pagecontract.ErrPreviewInvalidDocument, err)
	}
	// 预览不产出 Manifest，因此不记录依赖线索（usage 传 nil）。
	// 也不注入归因收集器：预览的归因体现在**占位上**（哪个槽位 / 哪个节点 / 哪份来源 /
	// 为什么没展开，见 data-sky-* 属性），没有 Manifest 可写。
	// 模式为预览：显式绑定但拿不到的结构依赖在这里不失败（编辑期配置不完整是常态），
	// 与发布路径共用同一份装配管线，这就是两者的唯一差异。
	html, err = s.compileDocument(ctx, page, projectID, currentPath, buildLang(lang), nil, false,
		builder.CompileModePreview, nil)
	if err != nil {
		if errors.Is(err, errCompileFailed) {
			logger.Scene("build").Error(err, "预览编译失败")
			// 用 %w 而不是 %v 保留错误链：把内层类型转成字符串会丢掉
			// pagecontract.PreviewProblem（作者可操作的组件校验提示），
			// 消费侧（workbench）就只能靠嗅探文本或压成一句泛化文案。
			// 文本形态与原来的 "%w: %v" 逐字相同（两个 %w 也按同样格式拼接）。
			return nil, fmt.Errorf("%w: %w", pagecontract.ErrPreviewCompileFailed, err)
		}
		logger.Scene("build").Error(err, "预览文档渲染失败")
		return nil, err
	}
	return html, nil
}
