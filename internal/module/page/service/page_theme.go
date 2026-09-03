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
	blockdto "go_wp/internal/module/block/dto"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
)

// mergeActiveTheme 把工程激活主题的设置合入页面文档：
// settings.theme（颜色/字体）与 settings.structure（页眉/页脚块绑定）。
// 无激活主题或查询失败时不合入（主题为可选增强，不阻塞页面保存）。
func (s *Service) mergeActiveTheme(ctx context.Context, projectID string, doc json.RawMessage) (json.RawMessage, error) {
	theme, err := s.project.GetActiveTheme(ctx, projectID)
	if err != nil || theme == nil {
		return doc, nil
	}
	var settings struct {
		Colors     json.RawMessage `json:"colors"`
		FontFamily string          `json:"fontFamily"`
		Header     string          `json:"headerBlockId"`
		Footer     string          `json:"footerBlockId"`
	}
	if len(theme.Settings) > 0 {
		if err := json.Unmarshal(theme.Settings, &settings); err != nil {
			logger.Scene("page").With("err", err).Warn("主题设置解析失败")
			return doc, nil // 主题设置非法时忽略
		}
	}
	// settings.theme 快照（仅当主题含颜色/字体设置）。
	if len(settings.Colors) > 0 || settings.FontFamily != "" {
		themeJSON, _ := json.Marshal(map[string]any{
			"colors":     settings.Colors,
			"fontFamily": settings.FontFamily,
		})
		if doc, err = mergeSettingsKey(doc, "theme", themeJSON); err != nil {
			return nil, err
		}
	}
	// settings.structure 快照（页眉/页脚块绑定）。
	structureJSON, _ := json.Marshal(map[string]any{
		"headerBlockId": settings.Header,
		"footerBlockId": settings.Footer,
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

// AttachThemeToUnassigned 把工程内未挂主题的页面挂到指定主题（工程首个主题创建后回填历史页面）。
func (s *Service) AttachThemeToUnassigned(ctx context.Context, projectID, themeID string) error {
	return s.model.AttachThemeToUnassigned(ctx, projectID, themeID)
}

// compileBlockFragment 编译单个全局块为片段（HTML/CSS）；块缺失或非法时降级为空片段。
// 绑定被删除的块不阻塞构建：页面产物退化为无页眉/页脚，保存主题绑定即可恢复。
func (s *Service) compileBlockFragment(ctx context.Context, blockID string) (html, css string) {
	if blockID == "" {
		return "", ""
	}
	block, err := s.blocks.Detail(ctx, &blockdto.DetailReq{ID: blockID})
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
	compiled, err := builder.Compile(page, builder.WithComponentSet(set))
	if err != nil {
		logger.Scene("build").With("block", blockID).Error(err, "块编译失败")
		return "", ""
	}
	logger.Scene("build").With("block", blockID).Info("块编译成功")
	return compiled.HTML, compiled.CSS
}
