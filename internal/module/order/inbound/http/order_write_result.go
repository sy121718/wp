package orderhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const orderErrScene = "order"

// orderReturnAutoReceive 把退货审核表单里的 autoReceive 读成布尔。
// checkbox 未勾选时浏览器根本不发这个键，所以「未出现」即 false。
func orderReturnAutoReceive(c *gin.Context) bool {
	switch strings.ToLower(strings.TrimSpace(c.PostForm("autoReceive"))) {
	case "1", "true", "on":
		return true
	}
	return false
}

// orderWriteOK 是写操作成功后的统一片段：只给「成功了」这一条事实，
// 需要整页刷新的表单由 Jet 在发起处声明 data-write-reload。
func orderWriteOK(c *gin.Context) {
	orderWriteResult(c, "ordSaved")
}

// orderWriteResult 渲染一条带词条 key 的操作结论片段。
func orderWriteResult(c *gin.Context, key string) {
	c.HTML(http.StatusOK, "admin/order/order_write_result.html", gin.H{"Key": key})
}

// orderWriteResultNav 渲染结论片段，并给出 Jet 声明的下一步链接。
// 链接由调用方按实体事实算好，前端只负责按 data-write-nav 导航。
func orderWriteResultNav(c *gin.Context, key, href string) {
	c.HTML(http.StatusOK, "admin/order/order_write_result.html", gin.H{"Key": key, "Href": href})
}

// orderWriteBadInput 把原生 binder / 表单校验的失败交给统一失败出口。
func orderWriteBadInput(c *gin.Context, key string) {
	orderWriteFail(c, facing.WithStatus(facing.New(key), http.StatusBadRequest))
}

// orderWriteFail 是 order 写端点的 HTML 失败出口：状态码取错误事实，
// 文案一律由失败模板按请求语言取词，不在这里产出成品句。
func orderWriteFail(c *gin.Context, err error) {
	pageview.FailStatus(c, response.ErrorStatus(err), orderErrScene, err)
}

// orderBulkResult 是批量写成功后的片段：只计数，逐条原因只在失败时出现。
func orderBulkResult(c *gin.Context, done, skipped int, rows []orderBulkResultRow) {
	c.HTML(http.StatusOK, "admin/order/order_bulk_result.html", gin.H{
		"None":    done == 0 && skipped == 0,
		"Done":    done,
		"Skipped": skipped,
		"Rows":    rows,
	})
}

// orderBulkResultRow 是批量结果里的一条失败事实。
type orderBulkResultRow struct {
	ID      string
	OK      bool
	Message string
}

// orderBulkFail 是批量写被整批拒绝时的失败片段：逐条原因由 Jet 逐条取词。
func orderBulkFail(c *gin.Context, items []facing.BlockedItem) {
	pageview.FailStatus(c, http.StatusConflict, orderErrScene,
		&orderBulkRejectedError{items: items})
}

type orderBulkRejectedError struct {
	items []facing.BlockedItem
}

func (e *orderBulkRejectedError) Error() string { return orderenums.ErrBulkRejected }

func (e *orderBulkRejectedError) Facing() (string, string) {
	return orderenums.ErrBulkRejected, ""
}

func (e *orderBulkRejectedError) BlockedItems() []facing.BlockedItem { return e.items }

// couponBackURL 是优惠码写成功后回到列表的地址：保留当前筛选上下文。
func couponBackURL(c *gin.Context, projectID string) string {
	return "/admin/coupons?" + couponListQuery(projectID,
		couponFilter{Status: c.Query("status"), Keyword: c.Query("keyword")}, 0, 0)
}

// orderNormalizeBulkIDs 归一化表单勾选的 id；超过上限按 400 打回。
func orderNormalizeBulkIDs(c *gin.Context, ids []string) ([]string, bool) {
	normalized, err := bulkids.Normalize(ids)
	if err != nil {
		orderWriteFail(c, err)
		return nil, false
	}
	if len(normalized) == 0 {
		orderWriteBadInput(c, orderenums.ErrInvalidParam)
		return nil, false
	}
	return normalized, true
}

// orderBulkFormIDs 从表单读 ids；完全没有这个键时返回 nil。
// 必须先解析表单：Gin 在 binder 之前不会填充 PostForm，直接读会让批量请求
// 被误判成单条请求（然后因 orderId 缺失整批 400）。
func orderBulkFormIDs(c *gin.Context) []string {
	if c.Request == nil {
		return nil
	}
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		if perr := c.Request.ParseForm(); perr != nil {
			return nil
		}
	}
	vals, ok := c.Request.PostForm["ids"]
	if !ok {
		return nil
	}
	return orderdto.OrderBulkIDs(vals)
}

// orderLogWriteError 记录写失败的内部原因，对外只给声明词条。
func orderLogWriteError(c *gin.Context, err error, message string) {
	logger.Scene(orderErrScene).With("user_id", c.GetInt64("user_id")).Error(err, message)
}

// orderFragmentData 给写回片段补齐请求级取词与 CSRF。
func orderFragmentData(c *gin.Context, data gin.H) gin.H {
	return adminui.Prepare(c, data)
}

// orderWriteTranslation 取一个词条的当前语言译文；取不到时返回 key 本身。
func orderWriteTranslation(c *gin.Context, key string) string {
	return response.TranslateMessage(c, key)
}
