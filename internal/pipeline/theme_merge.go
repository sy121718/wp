package pipeline

// theme_merge.go — 激活主题合入 Page Document（page / contenttemplate 共用，EDT-003）。
//
// 保存时把 settings.theme（主题令牌快照）与 settings.structure（页眉/页脚块绑定）
// 烘进文档，保证单页/单模板构建确定性（与 page.mergeActiveTheme 同口径）。

import (
	"context"
	"encoding/json"
	"fmt"

	"go_wp/internal/builder"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

// MergeActiveThemeIntoDocument 把工程激活主题合入文档 settings.theme / settings.structure。
func MergeActiveThemeIntoDocument(ctx context.Context, project projectcontract.ProjectService, projectID string, doc json.RawMessage) (json.RawMessage, error) {
	pageStructure, perr := ParseStructureBindings(doc)
	if perr != nil {
		pageStructure = builder.StructureBindings{}
	}
	if project == nil {
		return mergeThemeAbsent(doc, pageStructure)
	}
	theme, err := project.GetActiveTheme(ctx, projectID)
	if err != nil || theme == nil {
		return mergeThemeAbsent(doc, pageStructure)
	}
	var themeStructure struct {
		Header string `json:"headerBlockId"`
		Footer string `json:"footerBlockId"`
	}
	if len(theme.Settings) > 0 {
		if err := json.Unmarshal(theme.Settings, &themeStructure); err != nil {
			logger.Scene("build").With("err", err).Warn("主题设置解析失败")
			return mergeThemeAbsent(doc, pageStructure)
		}
	}
	themeSnapshot := json.RawMessage(`{}`)
	if merged := builder.MergeThemeRawJSON(theme.Settings, themeOverrideOf(doc)); len(merged) > 0 {
		themeSnapshot = merged
	}
	if doc, err = mergeSettingsKey(doc, "theme", themeSnapshot); err != nil {
		return nil, err
	}
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

// MergeStructureBindings 页面非空字段优先，空字段回落主题默认（与保存期 mergeActiveTheme 一致）。
func MergeStructureBindings(page, theme builder.StructureBindings) builder.StructureBindings {
	out := builder.StructureBindings{
		HeaderBlockID: page.HeaderBlockID,
		FooterBlockID: page.FooterBlockID,
	}
	if out.HeaderBlockID == "" {
		out.HeaderBlockID = theme.HeaderBlockID
	}
	if out.FooterBlockID == "" {
		out.FooterBlockID = theme.FooterBlockID
	}
	return out
}

func mergeThemeAbsent(doc json.RawMessage, pageStructure builder.StructureBindings) (json.RawMessage, error) {
	var err error
	if doc, err = mergeSettingsKey(doc, "theme", json.RawMessage(`{}`)); err != nil {
		return nil, err
	}
	structureJSON, _ := json.Marshal(map[string]any{
		"headerBlockId": pageStructure.HeaderBlockID,
		"footerBlockId": pageStructure.FooterBlockID,
	})
	return mergeSettingsKey(doc, "structure", structureJSON)
}

// ParseStructureBindings 从文档读取 settings.structure 页眉/页脚绑定。
func ParseStructureBindings(docJSON []byte) (builder.StructureBindings, error) {
	var page struct {
		Settings struct {
			Structure builder.StructureBindings `json:"structure"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(docJSON, &page); err != nil {
		return builder.StructureBindings{}, err
	}
	return page.Settings.Structure, nil
}

func themeOverrideOf(doc json.RawMessage) json.RawMessage {
	var page struct {
		Settings struct {
			ThemeOverride json.RawMessage `json:"themeOverride"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(doc, &page); err != nil {
		return nil
	}
	return page.Settings.ThemeOverride
}

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
