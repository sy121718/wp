package pageservice

// 站点主题与全局结构合入：激活主题 settings → 页面文档快照。
//   - settings.theme     ← 主题 colors/fontFamily（展示层快照，构建注入 :root 变量）
//   - settings.structure ← 主题 headerBlockId/footerBlockId（全局块槽位绑定快照，
//     构建期由 assembleCompile 内联页眉/页脚块）
// 页面保存/创建时快照合入（保证单页构建确定性）；
// 主题设置/绑定变更时由主题设置页触发批量刷新（RefreshThemeForTheme）。

import (
	"context"
	"encoding/json"
	"fmt"

	"go_wp/internal/builder"
	blockcontract "go_wp/internal/module/block/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
)

// mergeActiveTheme 把工程激活主题的设置合入页面文档：
// settings.theme（Woodmart 级主题令牌）与 settings.structure（页眉/页脚块绑定）。
// 页眉/页脚支持**页面级覆盖**：页面 settings.structure 非空字段优先（用户在
// 页面设置里显式选了某页眉/页脚），空字段回退主题默认（headerBlockId/
// footerBlockId 为空 = 用主题）。无激活主题时保留页面现有绑定。
func (s *Service) mergeActiveTheme(ctx context.Context, projectID string, doc json.RawMessage) (json.RawMessage, error) {
	// 页面现有 structure（页面级覆盖优先）。
	pageStructure, perr := parseStructureBindings(doc)
	if perr != nil {
		pageStructure = builder.StructureBindings{}
	}

	theme, err := s.project.GetActiveTheme(ctx, projectID)
	if err != nil || theme == nil {
		// 无主题：保留页面现有 structure（页面级选择），theme 写空快照保持键存在。
		if doc, err = mergeSettingsKey(doc, "theme", json.RawMessage(`{}`)); err != nil {
			return nil, err
		}
		structureJSON, _ := json.Marshal(map[string]any{
			"headerBlockId": pageStructure.HeaderBlockID,
			"footerBlockId": pageStructure.FooterBlockID,
		})
		return mergeSettingsKey(doc, "structure", structureJSON)
	}
	var themeStructure struct {
		Header string `json:"headerBlockId"`
		Footer string `json:"footerBlockId"`
	}
	if len(theme.Settings) > 0 {
		if err := json.Unmarshal(theme.Settings, &themeStructure); err != nil {
			logger.Scene("page").With("err", err).Warn("主题设置解析失败")
			// 非法主题设置：保留页面现有 structure，theme 写空快照。
			if doc, err = mergeSettingsKey(doc, "theme", json.RawMessage(`{}`)); err != nil {
				return nil, err
			}
			structureJSON, _ := json.Marshal(map[string]any{
				"headerBlockId": pageStructure.HeaderBlockID,
				"footerBlockId": pageStructure.FooterBlockID,
			})
			return mergeSettingsKey(doc, "structure", structureJSON)
		}
	}
	// settings.theme 快照：整体快照主题 settings（ThemeSettings Woodmart 级模型）。
	// 经 ParseThemeSettings 校验合法才快照，非法则空快照（不阻塞保存）。
	themeSnapshot := json.RawMessage(`{}`)
	if len(theme.Settings) > 0 {
		if _, perr := builder.ParseThemeSettings(theme.Settings); perr == nil {
			themeSnapshot = theme.Settings
		}
	}
	if doc, err = mergeSettingsKey(doc, "theme", themeSnapshot); err != nil {
		return nil, err
	}
	// settings.structure 合并：页面非空优先（覆盖），空字段回退主题默认。
	header := pageStructure.HeaderBlockID
	if header == "" {
		header = themeStructure.Header
	}
	footer := pageStructure.FooterBlockID
	if footer == "" {
		footer = themeStructure.Footer
	}
	structureJSON, _ := json.Marshal(map[string]any{
		"headerBlockId": header,
		"footerBlockId": footer,
	})
	return mergeSettingsKey(doc, "structure", structureJSON)
}

// mergeSettingsKey 深覆盖页面文档 settings 的单个键（theme/structure），其余键不动。
func mergeSettingsKey(doc json.RawMessage, key string, value json.RawMessage) (json.RawMessage, error) {
	var page struct {
		Settings map[string]json.RawMessage `json:"settings"`
		Root     json.RawMessage            `json:"root"`
	}
	if err := json.Unmarshal(doc, &page); err != nil {
		return nil, fmt.Errorf("页面文档解析失败: %w", err)
	}
	if page.Settings == nil {
		page.Settings = map[string]json.RawMessage{}
	}
	page.Settings[key] = value
	settingsBytes, err := json.Marshal(page.Settings)
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{
		"settings": settingsBytes,
		"root":     page.Root,
	}
	return json.Marshal(out)
}

// ActiveThemeID 取工程当前激活主题 ID；无主题或查询失败返回空串（不阻塞页面创建）。
func (s *Service) ActiveThemeID(ctx context.Context, projectID string) string {
	theme, err := s.project.GetActiveTheme(ctx, projectID)
	if err != nil || theme == nil {
		return ""
	}
	return theme.ID
}

// RefreshThemeForTheme 把主题设置批量合入挂在该主题下全部页面（主题设置保存后调用）。
// 只更新 settings.theme，不动 draftVersion 与 revision（主题是展示层，不是内容变更）。
func (s *Service) RefreshThemeForTheme(ctx context.Context, themeID string, theme json.RawMessage) error {
	return s.model.RefreshThemeForTheme(ctx, themeID, theme)
}

// RefreshStructureForTheme 把主题的页眉/页脚块绑定批量合入挂在该主题下全部页面。
// 主题换绑全局块后调用；页面已发布产物需重新构建才会带新结构。
func (s *Service) RefreshStructureForTheme(ctx context.Context, themeID string, structure json.RawMessage) error {
	return s.model.RefreshStructureForTheme(ctx, themeID, structure)
}

// MarkStaleForTheme 把挂在该主题下全部页面标记为待重建（页眉/页脚块内容变更后调用）。
func (s *Service) MarkStaleForTheme(ctx context.Context, themeID string) error {
	return s.model.MarkStaleForTheme(ctx, themeID)
}

// MarkStaleForBlock 把文档中经 core.globalref 引用或 settings.structure 页眉/页脚
// 自选绑定该块的页面标记为待重建（块内容变更后调用，与 MarkStaleForTheme 互补）。
func (s *Service) MarkStaleForBlock(ctx context.Context, blockID string) error {
	return s.model.MarkStaleForBlock(ctx, blockID)
}

// CountBlockReference 统计引用该块的未删除页面数（globalref / structure 自选绑定），
// 供 block 模块删除或切换 global→template 前的引用拦截（docs/02-D §9）。
func (s *Service) CountBlockReference(ctx context.Context, blockID string) (int64, error) {
	return s.model.CountBlockReference(ctx, blockID)
}

// AttachThemeToUnassigned 把工程内未挂主题的页面挂到指定主题（工程首个主题创建后回填历史页面）。
func (s *Service) AttachThemeToUnassigned(ctx context.Context, projectID, themeID string) error {
	return s.model.AttachThemeToUnassigned(ctx, projectID, themeID)
}

// ReattachProjectPagesToTheme 把工程内全部页面（含已挂其他主题的）转挂到指定主题。
// 切换激活主题时调用：使整站页面以新激活主题为键，后续批量刷新快照/标重建可命中全部页面。
func (s *Service) ReattachProjectPagesToTheme(ctx context.Context, projectID, themeID string) error {
	return s.model.ReattachProjectPagesToTheme(ctx, projectID, themeID)
}

// compileBlockFragment 编译单个全局块为片段（HTML/CSS）；块缺失或非法时降级为空片段。
// 绑定被删除的块不阻塞构建：页面产物退化为无页眉/页脚，保存主题绑定即可恢复。
func (s *Service) compileBlockFragment(ctx context.Context, blockID string) (html, css string) {
	if blockID == "" {
		return "", ""
	}
	block, err := s.blocks.Detail(ctx, &blockcontract.DetailReq{ID: blockID})
	if err != nil || block == nil || len(block.Document) == 0 {
		logger.Scene("build").With("block", blockID).Error(err, "页眉/页脚块不可用")
		return "", ""
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		logger.Scene("build").With("block", blockID).Error(err, "块文档解析失败")
		return "", ""
	}
	set, serr := templates.NewEmbeddedComponentSet()
	if serr != nil {
		logger.Scene("build").With("block", blockID).Error(serr, "组件模板 Set 加载失败")
		return "", ""
	}
	compiled, err := builder.Compile(page, builder.WithContext(ctx), builder.WithComponentSet(set))
	if err != nil {
		logger.Scene("build").With("block", blockID).Error(err, "块编译失败")
		return "", ""
	}
	logger.Scene("build").With("block", blockID).Info("块编译成功")
	return compiled.HTML, compiled.CSS
}
