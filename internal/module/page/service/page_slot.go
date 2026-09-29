package pageservice

// page_slot.go — 系统页面槽位：绑定、解绑与解析（BIZ-1 访问面骨架）。
//
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
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/pipeline"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	"go_wp/pkg/i18n"
)

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
