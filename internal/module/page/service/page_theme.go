package pageservice

// 站点主题合并：project.settings.theme → 页面文档 settings.theme。
// 页面保存/创建时快照合入（保证单页构建确定性）；
// 项目主题变更时由设置页触发批量刷新（UpdateThemeForAllPages）。

import (
	"context"
	"encoding/json"
	"fmt"

	projectdto "go_wp/internal/module/project/dto"
)

// mergeProjectTheme 从站点工程 settings 读取 theme 并合入页面文档 settings.theme。
func (s *Service) mergeProjectTheme(ctx context.Context, projectID string, doc json.RawMessage) (json.RawMessage, error) {
	proj, err := s.project.Detail(ctx, &projectdto.DetailReq{ID: projectID})
	if err != nil || proj == nil {
		// 工程缺失不阻塞页面保存（主题为可选增强）。
		return doc, nil
	}
	theme, err := extractTheme(proj.Settings)
	if err != nil || theme == nil {
		return doc, err
	}
	return mergeThemeIntoDoc(doc, theme)
}

// extractTheme 从 project settings JSON 提取 theme 字段。
func extractTheme(settings json.RawMessage) (json.RawMessage, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var s struct {
		Theme json.RawMessage `json:"theme"`
	}
	if err := json.Unmarshal(settings, &s); err != nil {
		return nil, nil // settings 非法时忽略主题
	}
	if len(s.Theme) == 0 {
		return nil, nil
	}
	return s.Theme, nil
}

// mergeThemeIntoDoc 把 theme 合入页面文档 settings.theme（深覆盖 settings 键，其余不动）。
func mergeThemeIntoDoc(doc json.RawMessage, theme json.RawMessage) (json.RawMessage, error) {
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
	page.Settings["theme"] = theme
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

// AttachThemeToUnassigned 把工程内未挂主题的页面挂到指定主题（工程首个主题创建后回填历史页面）。
func (s *Service) AttachThemeToUnassigned(ctx context.Context, projectID, themeID string) error {
	return s.model.AttachThemeToUnassigned(ctx, projectID, themeID)
}
