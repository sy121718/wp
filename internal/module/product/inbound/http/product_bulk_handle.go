package producthttp

// product_bulk_handle.go — 商品域批量端点的共享入口：id 归一 + 超限出口，以及批量结论的
// **文案事实**（key + 中文兜底，取词在出口）。
//
// 原先这些与「页面跳转 / 错误文案」混在一个 product_jump.go 里；跳转出口删干净之后，
// 剩下的就是这两件事，按它们真实服务的对象（批量端点）命名。

import (
	"errors"
	"fmt"
	"go_wp/internal/adminui"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// productBulkIDs 批量 id 的归一 + 超限出口（商品批量删除 / 批量改价 / 四套归类删除共用）。
//
// 超限**必须**把「上限」与「本次条数」两个数字给出来：只说「请求参数无效」时操作者不知道该拆成
// 几批，而这正是这条拒绝唯一可行动的信息。词条与模板都归 pkg/bulkids 所有（它的两个 %s 依次是
// 上限与本次条数），这里只做「按请求语言取成品串 + 注入数字」。
//
// 返回 false 表示已写出响应、调用方直接 return。空列表**不在这里拦**：没勾选是各端点自己的
// 语义（批量改价要一句专门的话，批量删除只要参数无效）。
func productBulkIDs(c *gin.Context, raw []string) ([]string, bool) {
	ids, err := bulkids.Normalize(raw)
	if err == nil {
		return ids, true
	}
	var limit *bulkids.IDsError
	if errors.As(err, &limit) {
		template := i18n.Translate(bulkids.MsgTooMany, bulkids.TooManyTemplate, response.RequestLanguage(c))
		response.ErrorWithMessage(c, http.StatusBadRequest, fmt.Sprintf(template, strconv.Itoa(limit.Max), strconv.Itoa(limit.Count)))
		return nil, false
	}
	response.ErrorWithMessage(c, http.StatusBadRequest, productenums.ErrInvalidParam)
	return nil, false
}

// productBulkText 一条结论文案的事实（key + 中文兜底）。
type productBulkText struct{ key, fallback string }

// fact 把一条结论文案变成**事实**（key + 中文兜底 + %s 参数）：模板在渲染期取词，
// JSON 出口用 adminui.MsgText 取成品串。
//
// 参数一律 strconv 成字符串（模板侧的占位符是 %s，传 int 会渲染成 %!s(int=3)）；
// 没有占位符的词条**不要**传参数 —— TranslateArgs 对多余参数会 Sprintf 出 %!(EXTRA string=…)。
func (t productBulkText) fact(args ...string) adminui.Msg {
	m := adminui.Msg{Key: t.key, Fallback: t.fallback}
	if len(args) > 0 {
		m = m.WithArgs(args...)
	}
	return m
}

// productBulkTextOf 取一条批量结论文案的当前语言模板（**文本**形态，给「多段拼一句」的合成句用）。
//
// 词条混进 %d 之类协议外占位符时回落中文原文（模板一律只允许 %s，数字先经 strconv.Itoa）——
// 否则 Sprintf 会把参数渲染成 int，而这条路直接给运营看。
func productBulkTextOf(c *gin.Context, t productBulkText) string {
	text := shell.TranslateFor(c)(t.key, t.fallback)
	if !i18n.HasStringPlaceholdersOnly(text) {
		return t.fallback
	}
	return text
}

// productActionDoneFallback 单条写动作的通用成功回执的中文兜底（key 在
// productenums.ProductsActionDone）。
//
// 不复用 enums 的 MsgCreateSuccess / MsgUpdateSuccess：那几个 key 的值在词条表里是
// 「Blueprint 创建成功」这类**蓝图域**的句子（058/430 早先 seed），商品页用它会显示成
// 「Blueprint 创建成功」。这里另起一条商品域 key（登记前走中文兜底）。
const productActionDoneFallback = "操作已完成"

// productActionDoneMsg 单条写动作（新建 / 修改 / 保存）的成功回执事实。
var productActionDoneMsg = adminui.Msg{Key: productenums.ProductsActionDone, Fallback: productActionDoneFallback}

// 变体清单保存的结论文案（详情页编辑表单的回执）。
var (
	productVariantSaveNoChange = productBulkText{productenums.BulkVariantNoChange, "变体清单与库里一致，没有需要保存的变化。"}
	productVariantSaveSaved    = productBulkText{productenums.BulkVariantSaved, "已保存变体清单：新增 %s 个、修改 SKU %s 个、删除 %s 个。"}
	productVariantSaveSkipped  = productBulkText{productenums.BulkVariantSkipped, "跳过 %s 个：%s"}
)
