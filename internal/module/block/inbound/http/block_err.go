package blockhttp

// block_err.go — 全局块写动作的**文案归口**（错误文案三件套）。
//
// 三件套（AGENTS.md §响应与错误处理）：
//
//	① 可透出性 —— 交给 adminui.PageErrorFact，判据是 pkg/response.IsBusinessError 的**形态**
//	   （enums 常量名 / `模块.类别.语义` 的 key）。原先这里是 blockErrSentinels + errors.Is，
//	   那是**第二份真相**：service 换了措辞、或某个 key 改由别的模块提供就静默失配；
//	② 归口文案 —— 未命中时返回 adminui.MsgInternalError（可翻译 key + 中文兜底）；
//	③ 结构化日志 —— 原文只进日志，带 user_id / path（在各写路径的调用点记）。
//
// 传输通道已两次收窄，这里记清楚现状，免得下一个人按旧形状找出口：
//   · **读侧**（?err= / ?done= 的受控文案集合与判定）整批删除 —— 那条通道要求每个模块
//     维护一份「受控文案 + 数字归一模板」来证明提示出自本仓，而查询参数不是可信边界；
//   · **页面写路由**整批删除 —— 新建 / 删除 / 批量删除现在由前端直调 JSON API
//     （static/js/ui/write-base.js），整页提示页（shell.RenderJump）那条通道随之退役。
//
// 于是本文件只剩两件事：
//   ① blockErrText / blockErrFact —— 错误 → 受控文案 / 展示事实（判定与归口在
//      adminui.PageErrorFact，模块不再持有 sentinel 白名单；SaveBlockContent 与
//      JSON 批量端点的逐条原因都用它）；
//   ② blockRefErrText / blockRefUsageText —— 删除被拒时把**引用明细**（哪一类引用、
//      哪些实体）拼进文案，让人知道去哪儿解除引用（页面与批量结果区共用同一份措辞）。
// 回跳地址（shell.BackPath + 提示页链接）已随页面写路由一起删除。

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	blockcontract "go_wp/internal/module/block/contract"
	blockenums "go_wp/internal/module/block/enums"
	"go_wp/pkg/i18n"
)

// blockRefDetailMaxBytes 引用明细允许占用的字节上限（**展示封顶**）。
//
// 提示页正文太长会挤掉「立即前往」的链接与其它提示；这里按可读性封顶。
// （原先是按读侧的 URL 形状判定反推出来的，那条判定已删除。）
const blockRefDetailMaxBytes = 420

// blockRefTextMaxBytes「受控文案 + ：+ 引用明细」整串的展示上限（字节）。
//
// 512 取自壳层原先的受控回执上限（那族协议已整族下线，task-67）—— 这里只借用它的量级：
// 整串进的是提示页正文，超长会把页面撑破；留 1 字节给 truncateRunes 收尾的「…」，
// 故使用时 −1（与原先引用该常量时的取值一致）。
// 为什么不继续跨包引用：那个常量所属的读侧协议已无任何消费方，留一个指向
// 「已不存在的协议」的引用，会让下一个读代码的人去找一段没有上下文的常量。
const blockRefTextMaxBytes = 512

// blockUsageKindKey 引用类别 → i18n key（真文案在 sys_i18n，迁移 297）。
//
// switch 穷举而不是拼字符串（"MsgBlockUsage" + ...）：类别是枚举，新增取值时
// 漏改这里会静默退回渲染成英文枚举值（"page_structure"），而它看起来"像"一个正常文案。
func blockUsageKindKey(kind blockcontract.BlockUsageKind) string {
	switch kind {
	case blockcontract.UsageKindPageDocument:
		return blockenums.MsgBlockUsagePageDocument
	case blockcontract.UsageKindPageStructure:
		return blockenums.MsgBlockUsagePageStructure
	case blockcontract.UsageKindPageRevision:
		return blockenums.MsgBlockUsagePageRevision
	case blockcontract.UsageKindThemeSlot:
		return blockenums.MsgBlockUsageThemeSlot
	case blockcontract.UsageKindBlockDocument:
		return blockenums.MsgBlockUsageBlockDocument
	case blockcontract.UsageKindContentTemplate:
		return blockenums.MsgBlockUsageContentTemplate
	case blockcontract.UsageKindPresentationInstance:
		return blockenums.MsgBlockUsagePresentationInstance
	default:
		return string(kind)
	}
}

// blockRefUsageText 把拒绝错误里的引用明细渲染成当前语言的定位串（无明细返回空串）。
//
// 同类引用合并成一行（类别 + 至多 3 个定位 + 「等 N 处」）：块引用常常一次命中同一类
// 的几十个页面，逐条铺开既超长又不可读 —— 而这条文案的用途恰恰是「让人立刻知道去哪解除引用」。
func blockRefUsageText(c *gin.Context, err error) string {
	usages := blockcontract.BlockUsages(err)
	if len(usages) == 0 {
		return ""
	}
	// 取词：事实 → 当前语言文本。原先这里是 shell.TranslateFor(c)，同一个动作走两条链
	// （一处 shell、一处 adminui）会让「换语言时只有半数文案跟着变」这种缺陷没有单一落点。
	// 形状仍是 func(key, fallback string) string：i18n.FillTranslate 要这个签名。
	tr := func(key, fallback string) string {
		return adminui.MsgText(c, adminui.Msg{Key: key, Fallback: fallback})
	}
	order := make([]blockcontract.BlockUsageKind, 0, len(usages))
	grouped := map[blockcontract.BlockUsageKind][]string{}
	for _, u := range usages {
		loc := strings.TrimSpace(u.Label)
		if loc == "" {
			loc = strings.TrimSpace(u.EntityID)
		}
		if detail := strings.TrimSpace(u.Detail); detail != "" {
			loc = loc + "（" + detail + "）"
		}
		if _, ok := grouped[u.Kind]; !ok {
			order = append(order, u.Kind)
		}
		grouped[u.Kind] = append(grouped[u.Kind], loc)
	}
	parts := make([]string, 0, len(order))
	for _, kind := range order {
		key := blockUsageKindKey(kind)
		locs := grouped[kind]
		shown := locs
		tail := ""
		if len(locs) > 3 {
			shown = locs[:3]
			tail = i18n.FillTranslate(tr, blockenums.UsageMore, " 等 {count} 处",
				map[string]string{"count": strconv.Itoa(len(locs))})
		}
		parts = append(parts, tr(key, key)+"："+strings.Join(shown, "、")+tail)
	}
	return truncateRunes(strings.Join(parts, "；"), blockRefDetailMaxBytes)
}

// truncateRunes 按字节上限截断（不切断多字节字符），超出时以「…」收尾。
func truncateRunes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// blockRefErrText 块删除被拒时的页面文案：受控文案 + "：" + 引用明细。
//
// 明细是可定位的数据（哪一类引用、哪些实体），受控文案来自白名单；整串按展示上限封顶。
func blockRefErrText(c *gin.Context, err error) string {
	msg := blockErrText(c, err)
	if detail := blockRefUsageText(c, err); detail != "" {
		msg = truncateRunes(msg+"："+detail, blockRefTextMaxBytes-1)
	}
	return msg
}

// blockRefErrorMessage 与 blockErrorMessage 同源，但**带引用明细**时改用 blockRefErrText。
//
// 用在哪：删除被拒（409）这类「操作者必须知道是哪些页面/资源挡住」的回应。
// 为什么不能直接用 err.Error()：那串来自 contract.BlockInUseError，形态是
// 「block.err.inUse: page_document /pricing (main); media_asset …」—— 引用类别是内部枚举值
// （page_document 这种），分隔符与括号也是另一套写法；而页面提示走的是 blockRefErrText
// （本地化类别名 + 全角冒号 + 「、」分隔 + 超量收尾）。同一次拒绝，批量接口逐条回的是后者、
// 单条删除回的是前者，用户看到的会是两套说法，且靠 err.Error() 拼出的半角「: 」还会被
// response.translate 当「key: detail」解析（前缀不是 key，最终原样下发 —— 能显示，但类别名
// 是未翻译的内部值）。
//
// 无明细（普通业务错误 / 内部错误）时完全退回 blockErrorMessage：日志与 500 兜底文案都照旧。
func blockRefErrorMessage(c *gin.Context, err error) string {
	if blockRefUsageText(c, err) == "" {
		return blockErrorMessage(err)
	}
	return blockRefErrText(c, err)
}
