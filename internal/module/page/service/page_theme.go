package pageservice

// 站点主题与全局结构合入：激活主题 settings → 页面文档快照。
//   - settings.theme     ← 主题 colors/fontFamily（展示层快照，构建注入 :root 变量）
//   - settings.structure ← 主题 headerBlockId/footerBlockId（全局块槽位绑定快照，
//     构建期由 assembleCompile 内联页眉/页脚块）
// 页面保存/创建时快照合入（保证单页构建确定性）；
// 主题设置/绑定变更时由主题设置页触发批量刷新（RefreshThemeForTheme）。

// 为什么需要它：购物车片段里的「去结算」、下单成功的「查看订单」、登录页与注册页的互跳，
// 都要知道「那个页面是哪一个」。以前这些链接要么不存在、要么硬编码路径 —— 换个站就得改代码。
//
// 绑的是**页面 id**，解析时才换算成当前语言的线上路径：页面改 URL 是常规操作
//（draft_path / active_path 都是可变列），绑路径会一改就失效；语言前缀由构建期决定，
// 绑死路径则一切语言就错。
//
// 解析结果只含**已发布**的槽位：绑定存在与访问面真的有产物是两件事 ——
// 页面可以是草稿、可以已下线，那时给空路径，让调用方降级而不是输出死链。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/page/dto"
	"go_wp/internal/module/page/enums"
	"go_wp/internal/module/page/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
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
// 返回值自 2026-09 起把命中 id 一并透出（此前只进日志）：调用方拿它触发自动重建 ——
// 块/主题变更曾经只标记不重建，而人工入口并不存在，结果是「线上一直跑旧字节、后台只显示
// 一堆待重建」。日志回执照旧（样本进日志、ids 进返回值，两件事互不替代）。
func (s *Service) MarkStaleForTheme(ctx context.Context, themeID string) (ids []string, err error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	hit := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		ids, merr := s.model.MarkStaleForTheme(ctx, projectID, themeID)
		if merr != nil {
			return nil, merr
		}
		hit.add(ids)
	}
	affected := hit.list()
	s.logStaleImpact(ctx, "theme:"+themeID, affected)
	return affected, nil
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
func (s *Service) MarkStaleForBlock(ctx context.Context, blockID string) (ids []string, err error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	hit := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		ids, merr := s.model.MarkStaleForBlock(ctx, projectID, blockID)
		if merr != nil {
			return nil, merr
		}
		hit.add(ids)
	}
	affected := hit.list()
	s.logStaleImpact(ctx, "block:"+blockID, affected)
	return affected, nil
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

// ListSiteSlots 列出全部槽位及其当前绑定状态。
//
// **未绑定的槽位也在列表里**：后台要一屏看全「哪些位置还空着」，
// 只返回已绑定的会让作者以为自己配完了。
func (s *Service) ListSiteSlots(ctx context.Context, req *pagedto.SiteSlotListReq) (res *pagedto.SiteSlotListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(pageenums.ErrInvalidParam)
	}
	lang := strings.TrimSpace(req.Lang)
	if lang == "" {
		lang = i18n.GetDefaultLang()
	}
	bindings, err := s.model.ListSiteSlots(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	pageIDs := make([]string, 0, len(bindings))
	for _, b := range bindings {
		pageIDs = append(pageIDs, b.PageID)
	}
	pages, err := s.model.FindPagesByIDs(ctx, req.ProjectID, pageIDs)
	if err != nil {
		return nil, err
	}
	pageOf := make(map[string]int, len(pages))
	for i := range pages {
		pageOf[pages[i].ID] = i
	}
	pubs, err := s.model.ListPublicationsByPages(ctx, pageIDs, lang)
	if err != nil {
		return nil, err
	}
	activeOf := make(map[string]string, len(pubs))
	for _, p := range pubs {
		activeOf[p.PageID] = p.ActivePath
	}

	res = &pagedto.SiteSlotListResp{Lang: lang, Total: len(pageenums.SiteSlotDefs), Items: make([]pagedto.SiteSlotItem, 0, len(pageenums.SiteSlotDefs))}
	boundOf := make(map[string]string, len(bindings))
	for _, b := range bindings {
		boundOf[b.Slot] = b.PageID
	}
	for _, def := range pageenums.SiteSlotDefs {
		// SlotName / Usage 承载的是 **i18n key**（见 pageenums.SiteSlotDef）：
		// service 层拿不到请求语言，中文兜底留在 enums 表里，由后台页面层取词。
		item := pagedto.SiteSlotItem{Slot: def.Key, SlotName: def.NameKey, Usage: def.UsageKey}
		if pageID, ok := boundOf[def.Key]; ok {
			item.Bound, item.PageID = true, pageID
			if idx, found := pageOf[pageID]; found {
				item.DraftPath = pages[idx].DraftPath
				item.Path = activeOf[pageID]
				item.Published = item.Path != ""
			} else {
				// 绑定还在、页面已经查不到了（被删或不属于本工程）：
				// 标成悬空而不是显示成正常状态 —— 悬空绑定会让链接指向一个不存在的页面。
				item.PageDeleted = true
			}
			res.BoundCount++
		}
		res.Items = append(res.Items, item)
	}
	return res, nil
}

// BindSiteSlot 把槽位绑定到页面。
func (s *Service) BindSiteSlot(ctx context.Context, req *pagedto.SiteSlotBindReq) (err error) {
	projectID, slot, pageID, err := siteSlotTarget(req)
	if err != nil {
		return err
	}
	// 这里本来就有工程（siteSlotTarget 解析出来的），直接按工程作用域取（DB-009 第四批）：
	// 不带作用域在换非超级角色后一律「页面不存在」，下面的跨工程校验也就无从谈起。
	page, err := s.model.GetByID(ctx, pageID, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(pageenums.ErrSlotPageMiss)
		}
		return err
	}
	// 跨工程的页面不能绑：绑定与页面都按工程隔离，混绑的后果是站点上线后链接指向别的工程的页面。
	if page.ProjectID != projectID || page.DeletedAt != nil {
		return errors.New(pageenums.ErrSlotPageMiss)
	}
	now := time.Now().UTC()
	// 换绑与「标记受影响页面待重建」落在同一个事务里：
	//   · 只换绑不标记 → 引用了该槽位的页面继续输出指向旧页面的链接（线上死链）；
	//   · 只标记不换绑 → 白重建一遍（产物字节不变）。
	// 两处都是库内写入（page_site_slots 与 pages），没有任何理由分属两个事务。
	return s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		if uerr := s.model.UpsertSiteSlotTx(ctx, tx, &pagemodel.SiteSlotEntity{
			ProjectID: projectID, Slot: slot, PageID: pageID,
			CreateTime: now, UpdatedAt: now,
		}); uerr != nil {
			return uerr
		}
		return s.markProjectStaleForSlotTx(ctx, tx, projectID, slot, now)
	})
}

// markProjectStaleForSlotTx 在**调用方的事务**内标记「引用了该槽位」的页面待重建
// （审计 VIS-006）。
//
// 依赖记录里的 site_slot 条目是构建期写入的（页面真的渲染过该槽位的链接），
// 因此「只标记受影响的页面」与「不漏标」是同一件事：查得到就精确标，查不到就退回全量。
//
// 影响面**限于本次换绑的工程**：槽位绑定按 project_id 隔离，一个工程的换绑不会让
// 另一个工程的页面产物过期；依赖键（槽位名）本身不带工程，跨工程扇出会把无关工程
// 全标一遍（而且在单事务里也撞 RLS —— 会话变量只有一个值）。
func (s *Service) markProjectStaleForSlotTx(ctx context.Context, tx *gorm.DB, projectID, slot string, now time.Time) error {
	ids, err := s.model.MarkStaleByDependencyTx(ctx, tx, projectID, pipeline.DepKindSiteSlot, slot, now)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		// 精确命中：影响面在这里记下来（只读，失败不影响换绑主流程）。
		// 兜底分支（没有任何页面登记过该槽位依赖）不记 —— 那是「按工程全量标记」，
		// 影响面就是全工程的页面清单，/admin/pages 的待重建区块本来就是它的反查面。
		s.logStaleImpact(ctx, "site_slot:"+slot, ids)
		return nil
	}
	// 没有任何页面登记过这个槽位依赖：可能确实没人用，也可能页面还没构建过
	// （依赖随构建写入）。这两种情况无法从依赖表区分，因此按工程兜底标记。
	return s.model.MarkStaleForProjectTx(ctx, tx, projectID, now)
}

// UnbindSiteSlot 解绑槽位（幂等：本来没绑也返回成功）。
func (s *Service) UnbindSiteSlot(ctx context.Context, req *pagedto.SiteSlotUnbindReq) (err error) {
	if req == nil {
		return errors.New(pageenums.ErrInvalidParam)
	}
	projectID := strings.TrimSpace(req.ProjectID)
	slot := strings.TrimSpace(req.Slot)
	if projectID == "" {
		return errors.New(pageenums.ErrInvalidParam)
	}
	if !pageenums.IsSiteSlot(slot) {
		return errors.New(pageenums.ErrInvalidSlot)
	}
	// 解绑同样是「该槽位的链接失效」：解绑与标记待重建同事务（见 markProjectStaleForSlotTx）。
	return s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		if _, derr := s.model.DeleteSiteSlotTx(ctx, tx, projectID, slot); derr != nil {
			return derr
		}
		return s.markProjectStaleForSlotTx(ctx, tx, projectID, slot, time.Now().UTC())
	})
}

// ResolveSitePages 解析「槽位 → 当前语言线上路径」，只含已发布的绑定。
//
// 构建期与片段层共用这一份解析：产物里的链接与运行时片段里的链接必须来自同一处，
// 各解一次迟早分叉，而分叉的表现是「静态页上的去结算能点、片段渲染出来的去结算是 404」。
//
// 未绑定或未发布一律不出现（调用方据此不输出链接），**不猜**默认值 ——
// 猜错的链接比没有链接难查得多。
func (s *Service) ResolveSitePages(ctx context.Context, projectID, lang string) (out map[string]string, err error) {
	out = map[string]string{}
	if strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	if strings.TrimSpace(lang) == "" {
		lang = i18n.GetDefaultLang()
	}
	bindings, err := s.model.ListSiteSlots(ctx, projectID)
	if err != nil || len(bindings) == 0 {
		return out, err
	}
	ids := make([]string, 0, len(bindings))
	// 一个页面可以被多个槽位引用（「个人中心页同时是订单页」是合理用法）。
	slotsOf := make(map[string][]string, len(bindings))
	for _, b := range bindings {
		ids = append(ids, b.PageID)
		slotsOf[b.PageID] = append(slotsOf[b.PageID], b.Slot)
	}
	pubs, err := s.model.ListPublicationsByPages(ctx, ids, lang)
	if err != nil {
		return out, err
	}
	for _, p := range pubs {
		path := strings.TrimSpace(p.ActivePath)
		if path == "" {
			continue
		}
		for _, slot := range slotsOf[p.PageID] {
			out[slot] = path
		}
	}
	return out, nil
}

// SiteSlotRefsOfPage 列出引用了某页面的槽位键（页面删除前的引用检查）。
//
// projectID 必填（DB-009 切角色收口）：page_site_slots 带 FORCE 策略，缺作用域时
// 非超级角色静默 0 行 —— 引用检查会一律答「没有槽位引用这个页面」，删除因此放行，
// 留下指向已删页面的槽位绑定（页面打开时槽位解析不出路径）。
func (s *Service) SiteSlotRefsOfPage(ctx context.Context, projectID, pageID string) (slots []string, err error) {
	list, err := s.model.ListSiteSlotsByPage(ctx, projectID, pageID)
	if err != nil {
		return nil, err
	}
	for _, b := range list {
		slots = append(slots, b.Slot)
	}
	return slots, nil
}

// siteSlotTarget 取出并校验绑定三要素（工程 / 槽位 / 页面）。
func siteSlotTarget(req *pagedto.SiteSlotBindReq) (projectID, slot, pageID string, err error) {
	if req == nil {
		return "", "", "", errors.New(pageenums.ErrInvalidParam)
	}
	projectID = strings.TrimSpace(req.ProjectID)
	slot = strings.TrimSpace(req.Slot)
	pageID = strings.TrimSpace(req.PageID)
	if projectID == "" || pageID == "" {
		return "", "", "", errors.New(pageenums.ErrInvalidParam)
	}
	if !pageenums.IsSiteSlot(slot) {
		return "", "", "", errors.New(pageenums.ErrInvalidSlot)
	}
	return projectID, slot, pageID, nil
}

// pageContextOf 按页面 ID 取所属站点工程 ID 与该语言的「逻辑访问路径」。
// 工程 ID 用于导航解析（缺失时绑定菜单位置的导航节点编译期显式报错）；
// 逻辑路径用于导航「当前项」高亮与 hreflang 互指——调用方会再经 sitePath 加语言前缀。
//
// 多语言 P3 修正：此前取 pages.active_path（可能已带语言前缀），再经 highlightPath
// 加一次前缀会得到 /en/en/about，导航高亮永远匹配不上；现在按语言取
// page_publications 的行并用同一 LangURLRule 剥掉语言前缀，未发布回退草稿路径
// （草稿路径本就是逻辑路径）。
//
// 默认语言现场解析。构建 / 重建口径请走 pageContextOfWithDefault ——「哪个语言不带前缀」
// 由默认语言决定，现场解析会让改过 is_default 之后的重建把 /en/about 当成「默认语言路径」
// 原样留下，再加一次前缀就变成 /en/en/about（产物互指指向不存在的地址，且没有任何报错）。
func (s *Service) pageContextOf(ctx context.Context, pageID, lang string) (projectID, logicalPath string) {
	return s.pageContextOfWithDefault(ctx, pageID, lang, "")
}

// pageContextOfWithDefault 用**给定**默认语言剥语言前缀（审计 I18N-01 冻结口径）。
//
// defaultLang 为空 = 现场解析（预览 / 后台口径，与改造前逐字一致）。
func (s *Service) pageContextOfWithDefault(ctx context.Context, pageID, lang, defaultLang string) (projectID, logicalPath string) {
	if strings.TrimSpace(pageID) == "" {
		return "", ""
	}
	// 逐工程定位（DB-009 第四批）：调用方只有 pageId；不带作用域的直查在换非超级角色后
	// 一律失败 —— 这里的失败是**静默降级**（导航高亮悄悄消失），比报错更难发现。
	page, err := s.locatePageInProjects(ctx, pageID)
	if err != nil || page == nil {
		return "", ""
	}
	logicalPath = page.DraftPath
	if pub, perr := s.model.GetPublication(ctx, pageID, buildLang(lang)); perr == nil && pub != nil && pub.ActivePath != "" {
		rule := s.langURLRuleOf(ctx, page.ProjectID)
		if dl := strings.TrimSpace(defaultLang); dl != "" {
			rule = pipeline.LangURLRuleForProjectWithDefault(ctx, s.project, page.ProjectID, dl)
		}
		logicalPath = rule.Strip(buildLang(lang), pub.ActivePath)
	}
	return page.ProjectID, logicalPath
}

var _ pagecontract.SitePageResolver = (*Service)(nil)
