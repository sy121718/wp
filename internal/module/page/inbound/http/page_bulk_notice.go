package pagehttp

// page_bulk_notice.go — 页面管理页批量操作的**受控回执**（shell.FacingNoticeSpec 的第一个生产调用方）。
//
// 为什么必须换成受控回执：AGENTS.md 对批量操作的要求是「结论按『成功 N / 跳过 M』回带，
// 不允许静默的部分成功」。旧形态（`?done=已删除 3 个页面`）虽然也带计数，但计数是**客户端可改的**
// —— 读侧把模板归一化后比对（数字换成占位符），于是 `?done=已删除 999999 个页面` 与真回执
// 长得一模一样，运营无法分辨。受控回执把「句子」交给词条、URL 里只留白名单 key 与**有上限的整数**，
// 伪造能改的只有范围内的计数，塞不进整句话。
//
// 两个槽位：全成功走 done（info 条），有跳过走 errWarn（警告条更显眼，用户下次会去看剩下那些）
// —— 与改造前的显示位置逐字一致，只是内容改成受控形状。

import (
	"net/url"

	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"
)

// 批量回执的词条 key（同时是白名单项）。
const (
	// noticeBulkNothing 没有任何可处理项（选中集合为空）。
	noticeBulkNothing = "admin.pages.bulk.nothing"
	// noticeBulkDeleted 全成功。
	noticeBulkDeleted = "admin.pages.bulk.deleted"
	// noticeBulkSkipped 全部跳过。
	noticeBulkSkipped = "admin.pages.bulk.skipped"
	// noticeBulkPartial 部分成功（成功 n / 跳过 m）—— 契约里最要紧的那条。
	noticeBulkPartial = "admin.pages.bulk.partial"
)

// bulkNoticeParams 计数参数声明：n（成功）/ m（跳过），上限就是单次批量上限。
var bulkNoticeParams = []shell.FacingNoticeParam{
	{Name: "n", Max: shell.MaxBulkIDs},
	{Name: "m", Max: shell.MaxBulkIDs},
}

// pageBulkNoticeDone 成功槽位（info 条）。
var pageBulkNoticeDone = shell.FacingNoticeSpec{
	Slot: "done",
	Keys: map[string]string{
		noticeBulkNothing: "批量操作：没有需要处理的页面。",
		noticeBulkDeleted: "批量操作：已删除 {n} 个页面。",
	},
	Params: bulkNoticeParams,
}

// pageBulkNoticeErr 跳过槽位（警告条）。
var pageBulkNoticeErr = shell.FacingNoticeSpec{
	Slot: "errwarn",
	Keys: map[string]string{
		noticeBulkSkipped: "批量操作：跳过 {m} 个页面（已不存在或无权限）。",
		noticeBulkPartial: "批量操作：已删除 {n} 个页面，跳过 {m} 个。",
	},
	Params: bulkNoticeParams,
}

// bulkNoticeQueryOf 写侧：把受控回执写进回跳 URL 的 query。
//
// warn 决定走哪个槽位（与改造前的显示位置一致：有跳过 → 警告条）。
// 返回 error 只有一个来源：**调用点写错了**（key 不在白名单 / 参数没声明 / 计数超上限）。
// 调用方据此回退到旧的整串回执路径 —— 宁可显示旧形状的一条提示，也不能让操作者
// 什么都看不到（那才是真正的静默部分成功）。
func bulkNoticeQueryOf(warn bool, key string, counts map[string]int) (*url.Values, bool) {
	q := url.Values{}
	spec := pageBulkNoticeDone
	if warn {
		spec = pageBulkNoticeErr
	}
	if err := shell.SetFacingNoticeQuery(q, spec, key, counts); err != nil {
		logger.Scene("page").With("key", key).Error(err, "批量回执写入失败（回退旧形态）")
		return nil, false
	}
	return &q, true
}

// pageBulkNoticeText 读侧：读回受控回执（无回执返回空串，由调用方回退旧的整串判定）。
//
// 为什么保留旧路径：本页还有别的写入口（单独删除 / 排期 / 重定向）仍在用 `?done=` 整串形态，
// 一次性全换会同时改动它们的读侧 —— 本批只把**批量操作**换成受控回执，旧形态的语义一个字不改
// （既有能力不减弱，只是新增了受控形状）。
func pageBulkNoticeText(c *gin.Context, spec shell.FacingNoticeSpec) string {
	return shell.FacingNoticeText(c, spec, "")
}
