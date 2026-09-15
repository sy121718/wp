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
	// 结构绑定从主题设置里取：既有字段 + slots（公告条 / 侧边栏等）。
	// 只读 header/footer 会让「主题里配了公告条」在保存文档时被丢掉 ——
	// 主题设置页显示已配置，页面上却什么都没有。
	var themeSettings struct {
		Header string            `json:"headerBlockId"`
		Footer string            `json:"footerBlockId"`
		Slots  map[string]string `json:"slots"`
	}
	if len(theme.Settings) > 0 {
		if err := json.Unmarshal(theme.Settings, &themeSettings); err != nil {
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
	merged := MergeStructureBindings(pageStructure, builder.StructureBindings{
		HeaderBlockID: themeSettings.Header,
		FooterBlockID: themeSettings.Footer,
		Slots:         themeSettings.Slots,
	})
	fields := map[string]any{
		"headerBlockId": merged.HeaderBlockID,
		"footerBlockId": merged.FooterBlockID,
	}
	if len(merged.Slots) > 0 {
		fields["slots"] = merged.Slots
	}
	structureJSON, _ := json.Marshal(fields)
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
	// Slots 逐键合并（页面写了某个槽位就用页面的，其余取主题）：整体替换会让
	// 「主题新增一个公告条」把页面自己配的侧边栏冲掉，而页面那头完全没有报错。
	merged := map[string]string{}
	for slot, id := range theme.Slots {
		if id != "" {
			merged[slot] = id
		}
	}
	for slot, id := range page.Slots {
		if id != "" {
			merged[slot] = id
		}
	}
	if len(merged) > 0 {
		out.Slots = merged
	}
	return out
}

func mergeThemeAbsent(doc json.RawMessage, pageStructure builder.StructureBindings) (json.RawMessage, error) {
	var err error
	if doc, err = mergeSettingsKey(doc, "theme", json.RawMessage(`{}`)); err != nil {
		return nil, err
	}
	fields := map[string]any{
		"headerBlockId": pageStructure.HeaderBlockID,
		"footerBlockId": pageStructure.FooterBlockID,
	}
	// 空 slots 不写进文档：留一个 "slots":null 只会让每份文档多一份噪音，
	// 而读取侧本来就按「空即无绑定」处理。
	if len(pageStructure.Slots) > 0 {
		fields["slots"] = pageStructure.Slots
	}
	structureJSON, _ := json.Marshal(fields)
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
