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
	"time"

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"

	"gorm.io/gorm"
)

// mergeActiveTheme 把工程激活主题的设置合入页面文档：
// settings.theme（Woodmart 级主题令牌）与 settings.structure（页眉/页脚块绑定）。
// 页眉/页脚支持**页面级覆盖**：页面 settings.structure 非空字段优先（用户在
// 页面设置里显式选了某页眉/页脚），空字段回退主题默认（headerBlockId/
// footerBlockId 为空 = 用主题）。无激活主题时保留页面现有绑定。
func (s *Service) mergeActiveTheme(ctx context.Context, projectID string, doc json.RawMessage) (json.RawMessage, error) {
	return pipeline.MergeActiveThemeIntoDocument(ctx, s.project, projectID, doc)
}

// ActiveThemeID 取工程当前激活主题 ID；无主题或查询失败返回空串（不阻塞页面创建）。
func (s *Service) ActiveThemeID(ctx context.Context, projectID string) string {
	theme, err := s.project.GetActiveTheme(ctx, projectID)
	if err != nil || theme == nil {
		return ""
	}
	return theme.ID
}

// RefreshThemeForTheme 主题设置保存后刷新挂在该主题下全部页面的主题快照。
// 只更新 settings.theme，不动 draftVersion 与 revision（主题是展示层，不是内容变更）。
//
// 逐页合成而不是一条 SQL 批量写：快照 = 主题 + 页面级覆盖，每页覆盖不同；
// 且页面没覆盖过的项必须跟着新主题走、覆盖过的项保持不变。
func (s *Service) RefreshThemeForTheme(ctx context.Context, themeID string, theme json.RawMessage) error {
	rows, err := s.model.ListThemePageSnapshots(ctx, themeID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		snapshot := json.RawMessage(`{}`)
		if merged := builder.MergeThemeRawJSON(theme, row.Override); len(merged) > 0 {
			snapshot = merged
		}
		// themeId 只读标识随快照落库：产物 :root 的 --sky-theme-id 与当前主题一致。
		snapshot = builder.InjectThemeID(snapshot, themeID)
		if err := s.model.UpdateThemeSnapshot(ctx, row.ID, snapshot); err != nil {
			return err
		}
	}
	return nil
}

// RefreshStructureForTheme 把主题的页眉/页脚块绑定合入挂在该主题下全部页面。
// 页面已显式绑定的 header/footer 不被覆盖（与 mergeActiveTheme 同口径）。
func (s *Service) RefreshStructureForTheme(ctx context.Context, themeID string, structure json.RawMessage) error {
	var themeBindings builder.StructureBindings
	if len(structure) > 0 {
		if err := json.Unmarshal(structure, &themeBindings); err != nil {
			return err
		}
	}
	rows, err := s.model.ListThemePageStructureSnapshots(ctx, themeID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		pageBindings := builder.StructureBindings{}
		if len(row.Structure) > 0 {
			if err := json.Unmarshal(row.Structure, &pageBindings); err != nil {
				return err
			}
		}
		merged := pipeline.MergeStructureBindings(pageBindings, themeBindings)
		structureJSON, err := json.Marshal(merged)
		if err != nil {
			return err
		}
		if err := s.model.UpdateStructureSnapshot(ctx, row.ID, structureJSON); err != nil {
			return err
		}
	}
	return nil
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

// ReskinProjectForTheme 激活主题后的整站换皮（四步同一事务，失败整体回滚）。
func (s *Service) ReskinProjectForTheme(ctx context.Context, projectID, themeID string, theme, structure json.RawMessage) error {
	var themeBindings builder.StructureBindings
	if len(structure) > 0 {
		if err := json.Unmarshal(structure, &themeBindings); err != nil {
			return err
		}
	}
	return s.model.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.model.ReattachProjectPagesToThemeTx(ctx, tx, projectID, themeID); err != nil {
			return err
		}
		rows, err := s.model.ListThemePageSnapshotsTx(ctx, tx, themeID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, row := range rows {
			snapshot := json.RawMessage(`{}`)
			if merged := builder.MergeThemeRawJSON(theme, row.Override); len(merged) > 0 {
				snapshot = merged
			}
			// themeId 只读标识随快照落库（VIS-008 主题绑定）。
			snapshot = builder.InjectThemeID(snapshot, themeID)
			if err := s.model.UpdateThemeSnapshotTx(ctx, tx, row.ID, snapshot, now); err != nil {
				return err
			}
		}
		structRows, err := s.model.ListThemePageStructureSnapshotsTx(ctx, tx, themeID)
		if err != nil {
			return err
		}
		// VIS-008 换主题语义：清空槽位绑定，不保留旧主题的排版结构。
		// 页面 settings.structure 整体重置为新主题的默认绑定（含空 = 全清），
		// 旧主题烘焙进快照的 header/footer/slots 一律丢弃；页面如需自定义，
		// 换主题后在页面设置里重新选择。令牌侧的 themeOverride 不受影响
		//（VIS-002 分区：结构清空不波及令牌覆盖）。
		// 注意：已发布产物是静态文件，本事务只标 stale（MarkStaleForThemeTx），
		// 重新构建/发布后新主题才对访客可见。
		structureJSON, err := json.Marshal(themeBindings)
		if err != nil {
			return err
		}
		for _, row := range structRows {
			if err := s.model.UpdateStructureSnapshotTx(ctx, tx, row.ID, structureJSON, now); err != nil {
				return err
			}
		}
		return s.model.MarkStaleForThemeTx(ctx, tx, themeID, now)
	})
}

// structureSlotOptions 把 settings.structure 快照转成编译期的结构槽位绑定（审计 VIS-001）。
//
// 空绑定不产生任何 opt：没有页眉页脚时产物与改造前逐字节一致（无绑定即零影响）。
func structureSlotOptions(s builder.StructureBindings) []builder.CompileOption {
	bindings := s.SlotBindings()
	if len(bindings) == 0 {
		return nil
	}
	slots := make([]builder.StructureSlot, 0, len(bindings))
	for _, slot := range builder.SortedSlots(bindings) {
		slots = append(slots, builder.StructureSlot{Slot: slot, BlockID: bindings[slot]})
	}
	return []builder.CompileOption{builder.WithStructureSlots(slots...)}
}
