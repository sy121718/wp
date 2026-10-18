// product_message.go — 写动作回执的**文案合成**（多段词条拼一句 / 具名占位符的情形）。
//
// 为什么在 service：控制器是编排出口（只用 dto + service + 返回业务层给的东西）。
// 「已保存…；跳过 N 条：…」这类复合句要先取词再拼接，属于「值的组装」—— 留在控制器里
// 就是控制器在算东西，而且每个写动作各拼一遍，措辞与分隔符各写一次。
//
// 取词函数由调用方（请求期）传入：语言是请求级的，service 不持有 gin.Context。
package productservice

import (
	"fmt"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
)

// variantSkipFallbacks 清单外删除被跳过时的中文兜底（key 见 productenums.VariantSkip*）。
//
// 这些 key 是「这一行没写进去」的原因，不是整批失败的错误 —— 过不了 facing 的业务 key 判据
// （取值就是常量名），所以兜底必须与它一起声明在同一处，由本文件负责取词。
var variantSkipFallbacks = map[string]string{
	productenums.VariantSkipHasStock:         "仍有库存，未删除（请先处理库存或改为停用）",
	productenums.VariantSkipReferenced:       "被 BOM 清单引用，未删除（请先解除引用）",
	productenums.VariantSkipDuplicated:       "与清单里前面的行重复，已忽略",
	productenums.VariantSkipVariantMissing:   "这一行对应的变体已不存在（可能已被别处删除）",
	productenums.VariantSkipBundleReferenced: "被捆绑成员引用，未删除（请先在捆绑配置里解除引用）",
	productenums.VariantSkipHasMovement:      "有过库存流水（已被订单或库存变动用过），未删除（请改为停用）",
}

// bulkText 一条结论文案的事实（key + 中文兜底）。
type bulkText struct{ key, fallback string }

// fact 把一条结论文案变成事实（key + 中文兜底 + %s 参数）：模板渲染期取词，JSON 出口用 text.MsgText。
func (t bulkText) fact(args ...string) facing.Msg {
	m := facing.Msg{Key: t.key, Fallback: t.fallback}
	if len(args) > 0 {
		m = m.WithArgs(args...)
	}
	return m
}

// bulkTextOf 取一条结论文案的当前语言模板（**文本**形态，给「多段拼一句」的合成句用）。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（模板一律只允许 %s，数字先成字符串）——
// 否则 Sprintf 会把参数渲染成 %!d(...)，而这条路直接给运营看。
func bulkTextOf(tr TranslateFunc, t bulkText) string {
	built := tr(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(built) {
		return t.fallback
	}
	return built
}

// 变体清单保存的结论文案（详情页编辑表单的回执）。
var (
	variantSaveNoChange = bulkText{productenums.BulkVariantNoChange, "变体清单与库里一致，没有需要保存的变化。"}
	variantSaveSaved    = bulkText{productenums.BulkVariantSaved, "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。"}
	variantSaveSkipped  = bulkText{productenums.BulkVariantSkipped, "跳过 %s 个：%s"}
)

// VariantSaveMsg 保存结果的回带结论（事实：单一情形给 key，多段拼接给成品串）。
//
// 「一条都没变」与「跳过若干」必须能区分：都写成「保存成功」时，用户无法判断是清单本来就
// 与库里一致，还是有行被拦下了（被拦下的行下一次还会出现在清单里）。
//
// 带跳过明细时是「两句词条 + 逐条原因」拼起来的复合句，一段 key 表达不了 —— 这一档给成品串
// （Fallback，Key 留空），与 page 模块 pageTranslationsSavedMsg 同一取舍。
func VariantSaveMsg(tr TranslateFunc, res *productdto.SaveVariantListResp) facing.Msg {
	if res == nil {
		return facing.Msg{Key: "MsgInternalError", Fallback: "系统内部错误，请稍后重试"}
	}
	if res.Created == 0 && res.Updated == 0 && res.Deleted == 0 && len(res.Skipped) == 0 {
		return variantSaveNoChange.fact()
	}
	parts := []string{fmt.Sprintf(bulkTextOf(tr, variantSaveSaved),
		fmt.Sprintf("%d", res.Created), fmt.Sprintf("%d", res.Updated), fmt.Sprintf("%d", res.Deleted))}
	if len(res.Skipped) > 0 {
		// 只报**原因**，不把「哪个 SKU / 什么规格」拼进结论：受控形态下（每条原因都来自
		// variantSkipFallbacks 的枚举）文案可读且不泄漏；被跳过的行本来就留在清单里。
		items := make([]string, 0, len(res.Skipped))
		for _, skip := range res.Skipped {
			items = append(items, tr(skip.Reason, variantSkipFallbacks[skip.Reason]))
		}
		parts = append(parts, fmt.Sprintf(bulkTextOf(tr, variantSaveSkipped),
			fmt.Sprintf("%d", len(res.Skipped)), strings.Join(items, "；")))
	}
	return facing.Msg{Fallback: strings.Join(parts, " ")}
}

// PricingAppliedMsg 应用调价成功的回执（前缀 + 计数 + 后缀三段拼一句）。
//
// 两条词条还是两段式：收成一条 %s 整句要改 seed，属于独立的文案迁移，不夹在这一步做。
func PricingAppliedMsg(tr TranslateFunc, changed int) string {
	return tr(productenums.ProductPricingAppliedLead, "已应用调价：本次改动 ") +
		fmt.Sprintf("%d", changed) +
		tr(productenums.ProductPricingAppliedTail, " 个变体，明细见下方留痕台账。")
}

// TranslationsSavedMsg 译文保存成功的回执（0 条 = 没有需要写入的变化）。
//
// ProductTranslationsSavedNote 用的是具名占位符 {n}（i18n.FillTranslate），不是 %s；
// 模板注入的 tf 只有位置参数，一段 key + Args 表达不了它。
func TranslationsSavedMsg(tr TranslateFunc, written int) facing.Msg {
	if written > 0 {
		return facing.Msg{Fallback: i18n.FillTranslate(tr, productenums.ProductTranslationsSavedNote,
			"已保存 {n} 条译文；译文变更已标记待重建（下次构建生效）。",
			map[string]string{"n": fmt.Sprintf("%d", written)})}
	}
	return facing.Msg{Key: productenums.ProductTranslationsSavedNone, Fallback: "没有需要写入的变化。"}
}
