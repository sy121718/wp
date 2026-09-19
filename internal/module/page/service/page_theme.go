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
	"strings"
	"time"

	"go_wp/internal/builder"
	blockcontract "go_wp/internal/module/block/contract"
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
//
// 逐工程扇出（DB-009 第三批）：入口只带 themeID，没有工程；pages 带 FORCE 策略，
// 不逐工程设作用域的话整段刷新在换非超级角色后变成空转（保存成功但快照一个都没写）。
func (s *Service) RefreshThemeForTheme(ctx context.Context, themeID string, theme json.RawMessage) error {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		rows, err := s.model.ListThemePageSnapshots(ctx, projectID, themeID)
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
			if err := s.model.UpdateThemeSnapshot(ctx, projectID, row.ID, snapshot); err != nil {
				return err
			}
		}
	}
	return nil
}

// RefreshStructureForTheme 把主题的页眉/页脚块绑定合入挂在该主题下全部页面。
// 页面已显式绑定的 header/footer 不被覆盖（与 mergeActiveTheme 同口径）。
//
// 逐工程扇出（DB-009 第三批）：同 RefreshThemeForTheme —— 入口没有工程，而 pages
// 带 FORCE 策略，漏作用域时这条刷新在换非超级角色后静默空转。
func (s *Service) RefreshStructureForTheme(ctx context.Context, themeID string, structure json.RawMessage) error {
	var themeBindings builder.StructureBindings
	if len(structure) > 0 {
		if err := json.Unmarshal(structure, &themeBindings); err != nil {
			return err
		}
	}
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		rows, err := s.model.ListThemePageStructureSnapshots(ctx, projectID, themeID)
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
			if err := s.model.UpdateStructureSnapshot(ctx, projectID, row.ID, structureJSON); err != nil {
				return err
			}
		}
	}
	return nil
}

// MarkStaleForTheme 把挂在该主题下全部页面标记为待重建（页眉/页脚块内容变更后调用）。
//
// 逐工程扇出（DB-009 第三批）：入口只有 themeID，说不出工程；pages 带 FORCE 策略，
// 不逐工程设作用域时这条 UPDATE 在换非超级角色后静默匹配 0 行 —— 现象是「换了主题
// 或改了页眉块，页面却一直不被标记待重建」，站点上继续跑旧产物。
//
// 影响面回执（逐页样本）：逐工程 UPDATE 返回的命中 id 在这里聚合去重，扇出结束后记一条
// 「本次影响 N 个页面 + 前 K 条标题 / 路径」的结构化日志（logStaleImpact）。整站标记原先
// 没有任何逐页凭据，读者只能看到一个全局 stale 计数 —— 而「主题一变全站都 stale」正是
// 那个计数最没有区分度的场景。
//
// 方法签名与 contract 不变（调用方依赖它）：样本只进日志，不出现在返回值里。
func (s *Service) MarkStaleForTheme(ctx context.Context, themeID string) error {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return err
	}
	hit := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		ids, merr := s.model.MarkStaleForTheme(ctx, projectID, themeID)
		if merr != nil {
			return merr
		}
		hit.add(ids)
	}
	s.logStaleImpact(ctx, "theme:"+themeID, hit.list())
	return nil
}

// MarkStaleForBlock 把文档中经 core.globalref 引用或 settings.structure 页眉/页脚
// 自选绑定该块的页面标记为待重建（块内容变更后调用，与 MarkStaleForTheme 互补）。
//
// 逐工程扇出（DB-009 第三批）：块 id 是跨工程语义（调用方只给 id），而作用域只能是
// 某一个具体工程 —— 逐个工程各设一次，命中集合是各工程之和，不做「不限工程」的默认。
//
// 影响面回执同 MarkStaleForTheme：逐工程命中的页面在扇出结束后聚合成一条
// 「N 个页面 + 前 K 条标题 / 路径」的日志 —— 一个全局 stale 计数回答不了
// 「改了这块，是哪些页面要重建」。
func (s *Service) MarkStaleForBlock(ctx context.Context, blockID string) error {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return err
	}
	hit := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		ids, merr := s.model.MarkStaleForBlock(ctx, projectID, blockID)
		if merr != nil {
			return merr
		}
		hit.add(ids)
	}
	s.logStaleImpact(ctx, "block:"+blockID, hit.list())
	return nil
}

// CountBlockReference 统计引用该块的未删除页面数（globalref / structure 自选绑定），
// 供 block 模块删除或切换 global→template 前的引用拦截（docs/02-D §9）。
//
// 逐工程扇出后求和（DB-009 第三批）：这是 block 删除前那道拦截的判据，漏作用域时
// 它会静默返回 0 —— 于是「有页面在引用」的块被安静删掉，线上页面开始缺块。
func (s *Service) CountBlockReference(ctx context.Context, blockID string) (int64, error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		n, cerr := s.model.CountBlockReference(ctx, projectID, blockID)
		if cerr != nil {
			return 0, cerr
		}
		total += n
	}
	return total, nil
}

// ListBlockSourceRefs 列出引用该块的页面（审计 ARCH-02）：逐条给出页面路径与命中通道
// （文档树 / settings.structure 的页眉·页脚·槽位绑定），供装配层合并成块删除保护的判据。
//
// 为什么另开一条而不是把 CountBlockReference 的返回值改成明细：那条的消费者是
// 块列表页的「影响面」列（只要一个数），改签名会连带动一片；而删除保护要的是
// 「哪一类引用、哪些实体」——两件事共用同一组 model 匹配条件，口径不会分叉。
//
// 逐工程扇出（DB-009 第三批）：与 CountBlockReference 同一理由 —— 块 id 说不出工程，
// 而 pages 带 FORCE 策略；漏作用域时这条查询静默返回空，删除保护会据此**放行**。
func (s *Service) ListBlockSourceRefs(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]blockcontract.BlockUsage, 0, 2)
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		rows, rerr := s.model.ListBlockSourceRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for i := range rows {
			row := rows[i]
			label := strings.TrimSpace(row.Path)
			if label == "" {
				label = row.ID
			}
			// 同一页面可同时走两条通道（正文插块 + 页眉绑定），各记一条：
			// 解除路径不同，合并成一条会让操作者以为改完一处就够。
			if row.InDocument {
				out = append(out, blockcontract.BlockUsage{
					Kind: blockcontract.UsageKindPageDocument, ProjectID: projectID, EntityID: row.ID, Label: label,
				})
			}
			if row.InStructure {
				out = append(out, blockcontract.BlockUsage{
					Kind: blockcontract.UsageKindPageStructure, ProjectID: projectID, EntityID: row.ID,
					Label: label, Detail: "header/footer/slots",
				})
			}
		}
		// 历史修订（ARCH-02 补齐）：当前草稿已经不再引用、但某一版修订仍引用。
		// 单独一类而不是并进 PageDocument：回滚是唯一会暴露断链的路径，处置方式不同。
		revs, rerr := s.model.ListBlockRevisionRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for i := range revs {
			label := strings.TrimSpace(revs[i].Path)
			if label == "" {
				label = revs[i].PageID
			}
			out = append(out, blockcontract.BlockUsage{
				Kind: blockcontract.UsageKindPageRevision, ProjectID: projectID, EntityID: revs[i].PageID,
				Label: label, Detail: fmt.Sprintf("revision v%d", revs[i].Version),
			})
		}
	}
	return out, nil
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
		rows, err := s.model.ListThemePageSnapshotsTx(ctx, tx, projectID, themeID)
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
			if err := s.model.UpdateThemeSnapshotTx(ctx, tx, projectID, row.ID, snapshot, now); err != nil {
				return err
			}
		}
		structRows, err := s.model.ListThemePageStructureSnapshotsTx(ctx, tx, projectID, themeID)
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
			if err := s.model.UpdateStructureSnapshotTx(ctx, tx, projectID, row.ID, structureJSON, now); err != nil {
				return err
			}
		}
		return s.model.MarkStaleForThemeTx(ctx, tx, projectID, themeID, now)
	})
}

// 结构槽位的编译期绑定已收口到 pipeline.BuildStructureSlots（结构模板优先、块绑定回退，
// 两条构建路径共用一份实现）：本文件原先的 structureSlotOptions 只认块绑定，绑了结构模板
// 的槽位会被它整个忽略 —— 留着就是第二个真相，故删除。
