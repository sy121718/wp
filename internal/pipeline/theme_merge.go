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
		// 结构模板绑定（页眉 / 页脚的多套之选，与块绑定同一层）：
		// **不读它 = 主题里选的结构模板永远到不了页面文档**，构建期只能看到旧块绑定，
		// 表现是「主题设置页显示已选新页眉，站点上还是旧的」。
		HeaderTemplate string            `json:"headerTemplateId"`
		FooterTemplate string            `json:"footerTemplateId"`
		SlotTemplates  map[string]string `json:"slotTemplates"`
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
		HeaderBlockID:    themeSettings.Header,
		FooterBlockID:    themeSettings.Footer,
		Slots:            themeSettings.Slots,
		HeaderTemplateID: themeSettings.HeaderTemplate,
		FooterTemplateID: themeSettings.FooterTemplate,
		SlotTemplates:    themeSettings.SlotTemplates,
	})
	structureJSON, _ := json.Marshal(structureFields(merged))
	return mergeSettingsKey(doc, "structure", structureJSON)
}

// MergeStructureBindings 页面非空字段优先，空字段回落主题默认（与保存期 mergeActiveTheme 一致）。
//
// **两个通道（块 id / 结构模板 id）各自按同一规则合并**：把模板通道漏掉的话，主题里
// 选的页眉结构模板在保存页面时被整段丢掉（页面文档里只剩块绑定），而主题设置页照旧
// 显示「已选新页眉」—— 站点上一直是旧页眉，且没有任何报错。
func MergeStructureBindings(page, theme builder.StructureBindings) builder.StructureBindings {
	out := builder.StructureBindings{
		HeaderBlockID:    page.HeaderBlockID,
		FooterBlockID:    page.FooterBlockID,
		HeaderTemplateID: page.HeaderTemplateID,
		FooterTemplateID: page.FooterTemplateID,
	}
	if out.HeaderBlockID == "" {
		out.HeaderBlockID = theme.HeaderBlockID
	}
	if out.FooterBlockID == "" {
		out.FooterBlockID = theme.FooterBlockID
	}
	if out.HeaderTemplateID == "" {
		out.HeaderTemplateID = theme.HeaderTemplateID
	}
	if out.FooterTemplateID == "" {
		out.FooterTemplateID = theme.FooterTemplateID
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
	// SlotTemplates 同口径逐键合并（页面显式写了的槽位模板优先）。
	mergedTpl := map[string]string{}
	for slot, id := range theme.SlotTemplates {
		if id != "" {
			mergedTpl[slot] = id
		}
	}
	for slot, id := range page.SlotTemplates {
		if id != "" {
			mergedTpl[slot] = id
		}
	}
	if len(mergedTpl) > 0 {
		out.SlotTemplates = mergedTpl
	}
	return out
}

// structureFields 把结构绑定渲染成文档 settings.structure 的字段集合。
//
// headerBlockId / footerBlockId 恒写（既有行为，空值也是显式空绑定）；模板通道与
// slots 只在非空时写：留一个 "slotTemplates":null 只会让每份文档多一份噪音，
// 而读取侧本来就按「空即无绑定」处理。
func structureFields(b builder.StructureBindings) map[string]any {
	fields := map[string]any{
		"headerBlockId": b.HeaderBlockID,
		"footerBlockId": b.FooterBlockID,
	}
	if len(b.Slots) > 0 {
		fields["slots"] = b.Slots
	}
	if b.HeaderTemplateID != "" {
		fields["headerTemplateId"] = b.HeaderTemplateID
	}
	if b.FooterTemplateID != "" {
		fields["footerTemplateId"] = b.FooterTemplateID
	}
	if len(b.SlotTemplates) > 0 {
		fields["slotTemplates"] = b.SlotTemplates
	}
	return fields
}

func mergeThemeAbsent(doc json.RawMessage, pageStructure builder.StructureBindings) (json.RawMessage, error) {
	var err error
	if doc, err = mergeSettingsKey(doc, "theme", json.RawMessage(`{}`)); err != nil {
		return nil, err
	}
	structureJSON, _ := json.Marshal(structureFields(pageStructure))
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
