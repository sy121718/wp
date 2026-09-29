// membership_page_util.go — 后台页面的行视图、下拉选项与分页助手（BIZ-3）。
//
// 与 membership_page_handle.go 分开：那个文件是「页面怎么组装」，本文件是「一个行 / 一个选项
// 长什么样」。行视图在这里定型的好处是列表与筛选栏不会各造一份形状
// （同一个等级在两处显示成不同文案，是这类页面最常见的静默不一致）。
package membershiphttp

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	membershipmodel "go_wp/internal/module/membership/model"
	"go_wp/internal/web/shell"
)

// 归属页的分页参数（与 service 的 maxPageSize 同口径：超过上限即截，而不是让请求方决定查询规模）。
const (
	assignmentsPageSize    = 20
	assignmentsMaxPageSize = 200
)

// membershipNoticeFallback 成功回执的中文兜底（词条缺失时显示它，而不是裸 key）。
//
// 与迁移 462a 的 seed 值逐条一致：兜底与词条不一致时，英文界面上会突然冒出一句中文，
// 而两边看起来都「有值」。
var membershipNoticeFallback = map[string]string{
	membershipenums.MsgTierCreated:      "等级已创建",
	membershipenums.MsgTierUpdated:      "等级已更新",
	membershipenums.MsgTierDeleted:      "等级已删除",
	membershipenums.MsgEntitlementSaved: "权益已保存",
	membershipenums.MsgAssignSet:        "已指定会员等级",
	membershipenums.MsgAssignUnlocked:   "已取消手工锁定（下次日结按消费额重算）",
}

// membershipNotice 取一条成功回执的当前语言文字（回带进 ?done=）。
func membershipNotice(c *gin.Context, key string) string {
	return shell.TranslateFor(c)(key, membershipNoticeFallback[key])
}

// membershipTierRowOf 等级响应 → 列表行视图。
//
// 换算与拼接都在这里做完：模板只渲染，不做判断（Jet 里做逻辑的代价是出错时整页 500）。
func membershipTierRowOf(tier *membershipdto.TierResp) membershipTierRow {
	row := membershipTierRow{
		ID:            tier.ID,
		Name:          tier.Name,
		SortOrder:     tier.SortOrder,
		ThresholdYuan: formatFenToYuan(tier.ThresholdAmount),
		IsDefault:     tier.IsDefault,
		Remark:        tier.Remark,
	}
	for _, ent := range tier.Entitlements {
		switch ent.Kind {
		case membershipenums.KindFreeShipping:
			row.FreeShipping = ent.ValueInt == 1
			row.HasEntitlement = true
		case membershipenums.KindDiscount:
			row.DiscountText = strconv.FormatInt(ent.ValueInt, 10) + "%"
			row.DiscountRaw = strconv.FormatInt(ent.ValueInt, 10)
			row.HasEntitlement = true
		}
	}
	return row
}

// membershipAssignRowOf 归属响应 → 列表行视图。
//
// tr 由调用点传入（不在这里现取）：行视图是纯函数，取词依赖请求上下文，
// 混在一起会让「同一行在不同语言下渲染成什么」变得不可单测。
func membershipAssignRowOf(tr func(key, fallback string) string, item *membershipdto.AssignmentResp) membershipAssignRow {
	key, fallback := membershipSourceLabelKey(item.Source)
	return membershipAssignRow{
		ID:         item.ID,
		UserID:     item.UserID,
		TierID:     item.TierID,
		TierName:   item.TierName,
		Source:     item.Source,
		SourceText: tr(key, fallback),
		IsManual:   item.Source == membershipmodel.SourceManual,
		AssignedAt: item.AssignedAt.Time().Format("2006-01-02 15:04:05"),
	}
}

// membershipSourceOptions 来源筛选下拉（空值 = 全部）。
//
// Selected 由服务端算好而不是让模板比较：Jet 的 if 只接受 bool，
// 在模板里写 `{{if s.Value == $.FilterSource}}` 既有根引用（$）的解析风险，
// 也把「哪个选项被选中」这条判断摊到模板里 —— 出错时整页 500。
func membershipSourceOptions(c *gin.Context, selected string) []gin.H {
	tr := shell.TranslateFor(c)
	options := []struct{ Value, Label string }{
		{"", tr("admin.common.filter.optionAll", "全部")},
		{membershipmodel.SourceAuto, tr(membershipenums.SourceLabelAuto, "自动重算")},
		{membershipmodel.SourceManual, tr(membershipenums.SourceLabelManual, "手工指定")},
	}
	out := make([]gin.H, 0, len(options))
	for _, opt := range options {
		out = append(out, gin.H{"Value": opt.Value, "Label": opt.Label, "Selected": opt.Value == selected})
	}
	return out
}

// membershipTierOptions 等级筛选 / 选择下拉（Selected 同样由服务端算好）。
func membershipTierOptions(c *gin.Context, tiers []*membershipdto.TierResp, selected int64) []gin.H {
	tr := shell.TranslateFor(c)
	out := make([]gin.H, 0, len(tiers)+1)
	out = append(out, gin.H{
		"Value": int64(0), "Label": tr("admin.common.filter.optionAll", "全部"), "Selected": selected == 0,
	})
	for _, tier := range tiers {
		out = append(out, gin.H{"Value": tier.ID, "Label": tier.Name, "Selected": tier.ID == selected})
	}
	return out
}

// entitlementsFromForm 把表单里的两个固定权益字段转成 service 入参。
//
// 「字段不存在 = 这一条权益不设」是有意的：全量保存的语义就是「没列出的会被删掉」，
// 于是「取消免运费」不需要一个单独的删除动作 —— 运营把勾去掉再保存即可。
func entitlementsFromForm(c *gin.Context) []membershipdto.EntitlementReq {
	forms := parseEntitlementsFromForm(c)
	out := make([]membershipdto.EntitlementReq, 0, len(forms))
	for _, form := range forms {
		if !form.Enabled {
			continue
		}
		out = append(out, membershipdto.EntitlementReq{Kind: form.Kind, ValueInt: form.ValueInt})
	}
	return out
}

// normalizePageParams 读分页参数（缺省每页 20，上限 200）。
func normalizePageParams(c *gin.Context) (page, limit int) {
	page = formInt(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	limit = formInt(c.Query("limit"), assignmentsPageSize)
	if limit < 1 {
		limit = assignmentsPageSize
	}
	if limit > assignmentsMaxPageSize {
		limit = assignmentsMaxPageSize
	}
	return page, limit
}

// clampPage 把页码收敛到有效范围（先计数、再收敛、最后取当页 —— 顺序不能反：
// 先取数再收敛会让「页码越界」表现为空列表而不是最后一页）。
func clampPage(page, limit int, total int64) int {
	if limit <= 0 {
		return 1
	}
	maxPage := int((total + int64(limit) - 1) / int64(limit))
	if maxPage < 1 {
		return 1
	}
	if page > maxPage {
		return maxPage
	}
	return page
}

// itoa64 / itoaU64 拼分页链接用（0 值与空串在 URL 上都表示「不筛」，两处保持同一形态）。
func itoa64(v int64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatInt(v, 10)
}

// itoaU64 同上。
func itoaU64(v uint64) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatUint(v, 10)
}

// trimSpace 去首尾空白（工程 id 从 query / 表单进来）。
func trimSpace(raw string) string { return strings.TrimSpace(raw) }

// membershipTierEditOf 从等级清单里取出待编辑的那一条（id 为 0 或查不到时返回 nil）。
//
// 查的是**同一个工程的清单**（而不是再查一次库）：清单已经在手上，
// 而且「编辑区里的值」与「列表里显示的值」因此必然同源 —— 两处各查一次时，
// 并发编辑下会出现「列表显示旧值、编辑框显示新值」。
func membershipTierEditOf(list []*membershipdto.TierResp, tierID int64) *membershipTierEdit {
	if tierID <= 0 {
		return nil
	}
	for _, tier := range list {
		if tier.ID != tierID {
			continue
		}
		edit := &membershipTierEdit{
			ID:            tier.ID,
			Name:          tier.Name,
			SortOrder:     tier.SortOrder,
			ThresholdYuan: formatFenToYuan(tier.ThresholdAmount),
			Remark:        tier.Remark,
			IsDefault:     tier.IsDefault,
		}
		for _, ent := range tier.Entitlements {
			switch ent.Kind {
			case membershipenums.KindFreeShipping:
				edit.FreeShipping = ent.ValueInt == 1
			case membershipenums.KindDiscount:
				edit.DiscountPercent = strconv.FormatInt(ent.ValueInt, 10)
			}
		}
		return edit
	}
	return nil
}
