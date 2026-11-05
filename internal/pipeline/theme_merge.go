package pipeline

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"

	"go_wp/pkg/builder"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

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

	var themeSettings struct {
		Header string            `json:"headerBlockId"`
		Footer string            `json:"footerBlockId"`
		Slots  map[string]string `json:"slots"`

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
