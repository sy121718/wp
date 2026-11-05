package purchasehttp

import (
	"errors"
	"net/http"

	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	projectcontract "go_wp/internal/module/project/contract"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	"go_wp/internal/shell"
	"go_wp/internal/text"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const purchaseErrScene = "purchase"

func purchaseErrMsg(c *gin.Context, err error) facing.Msg {
	return purchaseErrMsgOf(c, text.FactOf(c, purchaseErrScene, err))
}

func purchaseErrMsgOf(c *gin.Context, f facing.Fact) facing.Msg {
	if f.Key == "" {
		return facing.Msg{}
	}
	if !f.Business {
		return response.InternalMsg()
	}
	if f.Tail != "" {
		if detail := purchaseErrDetailText(purchaseTranslate(c), f.Tail); detail != "" {
			return facing.Msg{Fallback: response.ArgsText(c, f.Key, purchaseErrFallback(f.Key), nil) + "：" + detail}
		}
	}
	return facing.Msg{Key: f.Key, Fallback: purchaseErrFallback(f.Key)}
}

func purchaseErrText(c *gin.Context, err error) string {
	return purchaseMsgText(c, purchaseErrMsg(c, err))
}

func purchaseErrDetailText(tr func(key, fallback string) string, tail string) string {
	parts, ok := i18n.ParseErrorDetails(tail)
	if !ok {
		return tail
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		fallback, registered := purchaseenums.ErrDetailFallbacks[p.Key]
		if !registered || fallback == "" {
			logger.Scene(purchaseErrScene).With("detail_key", p.Key).
				Warn("业务错误的补充说明词条未登记，已省略该段")
			continue
		}
		out = append(out, i18n.FillTranslate(tr, p.Key, fallback, p.Args))
	}
	return strings.Join(out, "；")
}

func purchaseErrFallback(key string) string { return key }

func purchaseMsgText(c *gin.Context, m facing.Msg) string { return response.MsgText(c, m) }

func purchaseTranslate(c *gin.Context) func(key, fallback string) string {
	return shell.TranslateFor(c)
}

var (
	purchaseSourceBackKeys = []string{"project", "type", "relatedParty", "status", "keyword", "page", "limit"}
	purchaseOrderBackKeys  = []string{"project", "status", "sourceId", "keyword", "page", "limit"}
)

func purchaseFail(c *gin.Context, err error) {
	// HTML 界面：把已映射的业务事实（key + 兜底）交给 Jet 取词。
	msg := purchaseErrMsg(c, err)

	if msg.Key == "" && msg.Fallback == "" {
		pageview.FailStatus(c, response.ErrorStatus(err), purchaseErrScene, err)
		return
	}
	key := msg.Key
	if len(msg.Args) > 0 {
		key += "|" + strings.Join(msg.Args, "|")
	}
	pageview.FailStatus(c, response.ErrorStatus(err), purchaseErrScene, facing.WithStatus(facing.New(key), response.ErrorStatus(err)))
}

// purchaseFailMsg 把已声明的消息事实交给失败出口：HTML 由 Jet 取词，数据接口仍走 JSON。
func purchaseFailMsg(c *gin.Context, msg facing.Msg) {
	key := msg.Key
	if len(msg.Args) > 0 {
		key += "|" + strings.Join(msg.Args, "|")
	}
	pageview.FailStatus(c, http.StatusBadRequest, purchaseErrScene, facing.New(key))
}

func purchaseBulkIDsMsg(c *gin.Context, err error) facing.Msg {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		if err != nil {
			logger.Scene(purchaseErrScene).
				With("path", c.Request.URL.Path).
				Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
		}
		return response.InternalMsg()
	}
	logger.Scene(purchaseErrScene).
		With("path", c.Request.URL.Path).
		With("count", tooMany.Count).
		With("max", tooMany.Max).
		Warn("批量操作被拒：提交的 id 数量超过上限")
	return facing.Msg{Key: bulkids.MsgTooMany, Fallback: bulkids.TooManyTemplate}.
		WithArgs(strconv.Itoa(tooMany.Max), strconv.Itoa(tooMany.Count))
}

var purchaseBulkSourcePartial = facing.Msg{
	Key:      purchaseenums.KeyBulkSourcePartial,
	Fallback: "已删除 %s 个，%s 个未能删除（仍被采购单或历史流水引用）",
}

func projectOptions(in []projectcontract.ProjectResp) []purchasedto.ProjectOptionResp {
	out := make([]purchasedto.ProjectOptionResp, 0, len(in))
	for _, p := range in {
		out = append(out, purchasedto.ProjectOptionResp{ID: p.ID, Name: p.Name})
	}
	return out
}

func purchasePageParams(rawPage, rawLimit, defaultSize, maxSize int) (page, limit int) {
	page = rawPage
	if page < 1 {
		page = 1
	}
	limit = rawLimit
	if limit < 1 {
		limit = defaultSize
	}
	if limit < 1 {
		limit = defaultSize
	}
	if limit > maxSize {
		limit = maxSize
	}
	return page, limit
}

func parseIntOr(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	got, err := strconv.Atoi(raw)
	if err != nil || got < 1 {
		return fallback
	}
	return got
}

func clampPurchasePage(page, limit int, total int64) int {
	if limit < 1 {
		limit = 1
	}
	if page < 1 {
		page = 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages < 1 {
		return 1
	}
	if page > pages {
		return pages
	}
	return page
}

func outstandingTone(outstanding int) string {
	if outstanding > 0 {
		return "warn"
	}
	return "ok"
}

func statusLabel(tr func(key, fallback string) string, status string) string {
	if status == purchaseenums.StatusDisabled {
		return tr(purchaseenums.KeyStatusDisabled, "已停用")
	}
	return tr(purchaseenums.KeyStatusActive, "启用中")
}

func isHXRequest(c *gin.Context) bool {
	return reqparams.IsHXRequest(c)
}

func operatorFromContext(c *gin.Context) (id string) {
	if name := strings.TrimSpace(builtin.GetUsername(c)); name != "" {
		return name
	}
	value, exists := c.Get("user_id")
	if !exists {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case interface{ String() string }:
		return v.String()
	default:
		return ""
	}
}
