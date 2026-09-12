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

	"github.com/google/uuid"
	"gorm.io/gorm"

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
	pages, err := s.model.FindPagesByIDs(ctx, pageIDs)
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
		item := pagedto.SiteSlotItem{Slot: def.Key, SlotName: def.Name, Usage: def.Usage}
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
	page, err := s.model.GetByID(ctx, pageID)
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
	if err = s.model.UpsertSiteSlot(ctx, &pagemodel.SiteSlotEntity{
		ID: uuid.NewString(), ProjectID: projectID, Slot: slot, PageID: pageID,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return err
	}
	// 槽位变了 → 把槽位路径烘进链接的产物全部过期。
	// 精确影响集合只有构建期才知道（页面文档里没有「我用了哪些槽位」的声明），
	// 所以按工程全量标记；这个入口的频率是「人工点保存」，不是热路径。
	return s.model.MarkStaleForProject(ctx, projectID, now)
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
	if _, err = s.model.DeleteSiteSlot(ctx, projectID, slot); err != nil {
		return err
	}
	return s.model.MarkStaleForProject(ctx, projectID, time.Now().UTC())
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
func (s *Service) SiteSlotRefsOfPage(ctx context.Context, pageID string) (slots []string, err error) {
	list, err := s.model.ListSiteSlotsByPage(ctx, pageID)
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
