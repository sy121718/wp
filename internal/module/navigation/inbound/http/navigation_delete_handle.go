// navigation_delete_handle.go — 删除端点的**严格形状 union**（单档 / 批档）。
//
// 导航列表页的「删除」与「批量删除」两条页面 POST（曾借同一个 /api/navigation/delete）已
// 迁到真实 JSON 端点，两条动作**不拆成两条路径**：拆开会让「能删一个、不能删十个」这种
// 状态出现，也会多出一个权限点与一条 api_path（sys_permission 每 code 只有一列 api_path）
// —— 于是同一个删除权限在角色授权下出现两套锚点。
//
// 判别键不是 action，而是**形状**：
//   - 单档只认 id（列表行内删除）；
//   - 批档只认 ids（勾选后批量删除），去重 / 上限 / 逐条结果沿用 pkg/bulkids 与 block 的口径。
//
// 两者混用、出现未知键、或 values 类型不对，一律 400 —— 猜分支会删错数据，而删除没有撤销。
//
// 逐条失败原因（items[].message）在**这里**按请求语言成品化：service 只给事实（业务 key
// 或原始错误），取词与缺词兜底都在 HTTP 出口做（见 navigationReasonText）。理由与
// navigation_error_handle.go 文件头的「两个出口取词归属不同」是同一件事：同一个错误在 JSON 接口、
// 页面提示条、将来的邮件里都要各按各的语言渲染，service 里翻一遍等于把语言钉死在服务层。
package navigationhttp

import (
	"bytes"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"

	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// navigationDeleteSingleKeys 单档允许出现的键。
var navigationDeleteSingleKeys = map[string]bool{"id": true}

// navigationDeleteBulkKeys 批档允许出现的键。
var navigationDeleteBulkKeys = map[string]bool{"ids": true}

// navigationBulkDeleteItem 批量删除的逐条结果。
//
// 字段名与 block 的批量结果同形（deleted/skipped/items + items[].message）：前端的
// json_write.js 按 data.deleted / data.skipped 计数、按 items[].message 列出逐条原因，
// 两个模块给不同的字段名就等于让基座维护两套读法。
type navigationBulkDeleteItem struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
	Message string `json:"message,omitempty"`
}

// navigationBulkDeleteResult 批量删除的整体回执。
type navigationBulkDeleteResult struct {
	Deleted int                        `json:"deleted"`
	Skipped int                        `json:"skipped"`
	Items   []navigationBulkDeleteItem `json:"items"`
}

// navigationDeleteHasKey 判断顶层是否出现某个键（出现即参与形状判别）。
func navigationDeleteHasKey(raw map[string]json.RawMessage, key string) bool {
	_, ok := raw[key]
	return ok
}

// navigationDeleteShapeOK 顶层键集合必须落在这条分支的白名单里。
//
// 严格是刻意的：`{"id":"x","ids":["y"]}` 这种混形如果按某一边「猜」着执行，被删掉的可能
// 恰恰是调用方以为不会删的那一批；未知键则意味着调用方按另一份契约在说话，这时执行单边
// 语义等于拿一次删除去赌对方的意思。
func navigationDeleteShapeOK(raw map[string]json.RawMessage, allow map[string]bool) bool {
	for key := range raw {
		if !allow[key] {
			return false
		}
	}
	return true
}

// navigationDeleteBulkIDsOf 批档的 ids：必须是字符串数组（null 项与非字符串一律拒）。
//
// 「缺省」与「显式 null」是两回事：`encoding/json` 把 null 解进 []string **不报错**（落到 nil），
// 于是 `{"ids":null}` 会静默变成一次空批量 —— 调用方以为发了一批，服务端什么也没做，
// 回执却是 200。这一档按形状错处理（400），数组元素里的 null（`["a", null]`）同理：
// 静默把 null 当空串再靠 Normalize 丢掉，等于替调用方猜意图。
// 空数组 `[]` 是**合法**的「没勾选任何项」：形状上说得通，回执 deleted=0，页面据此显示未勾选提示。
func navigationDeleteBulkIDsOf(raw json.RawMessage) ([]string, bool) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if bytes.Equal(bytes.TrimSpace(item), []byte("null")) {
			return nil, false
		}
		var text string
		if err := json.Unmarshal(item, &text); err != nil {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

// navigationBulkTooManyMessage 超限文案的**带参协议形态**（key|max|count）。
//
// 与 block / admin i18n 同源：词条与两个数字都归 pkg/bulkids（两个 %s 依次是上限与本次条数），
// response 的 translate 按 | 后的参数依次填参；页面批量的受控文案用的是同一条词条，
// 两个入口的提示不会各说各话。删除与来源批量加入两个动作共用它 —— 上限这件事说的是
// pkg/bulkids 的规则，与动作是什么无关。
func navigationBulkTooManyMessage(err error) string {
	var tooMany *bulkids.IDsError
	if !errors.As(err, &tooMany) {
		return bulkids.MsgTooMany
	}
	return fmt.Sprintf("%s|%d|%d", bulkids.MsgTooMany, tooMany.Max, tooMany.Count)
}

// 逐条原因的成品句与缺词兜底统一归口在 navigation_error_handle.go 的 navigationReasonText：
// 删除与「来源批量加入」两个动作的原因列是同一件事（按请求语言出成品句、缺词不裸 key），
// 两份实现等于让同一件事有两个版本。本文件只负责把错误事实交给它。
// deleteBulk 逐条删除：某一条失败只计入 skipped 并回带原因，整批不中断。
//
// 与页面批量删除的同一口径：整批回滚会让用户以为「一个都没删」，然后反复重试；
// 逐条走的是与单条删除**同一个** service 方法（「子项一并删除」的规则由 service 决定，
// handler 不复制一套）。
func (h *Handle) deleteBulk(c *gin.Context, raw map[string]json.RawMessage) {
	params, ok := navigationDeleteBulkIDsOf(raw["ids"])
	if !ok {
		response.ParamError(c)
		return
	}
	ids, nerr := bulkids.Normalize(params)
	if nerr != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, navigationBulkTooManyMessage(nerr))
		return
	}
	res := navigationBulkDeleteResult{Items: []navigationBulkDeleteItem{}}
	for _, id := range ids {
		item := navigationBulkDeleteItem{ID: id}
		if derr := h.svc.Delete(c.Request.Context(), &navigationdto.DeleteReq{ID: id}); derr != nil {
			logger.Scene("navigation").With("id", id).Error(derr, "批量删除导航项失败")
			item.Message = navigationReasonText(c, derr)
			res.Skipped++
			res.Items = append(res.Items, item)
			continue
		}
		item.Deleted = true
		res.Deleted++
		res.Items = append(res.Items, item)
	}
	response.Success(c, res)
}
