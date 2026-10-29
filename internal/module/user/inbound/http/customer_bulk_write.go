package userhttp

import (
	"errors"
	"fmt"
	"go_wp/pkg/binding"
	"net/http"
	"strconv"

	"go_wp/internal/adminui"
	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

type customerBulkItem struct {
	ID       uint64 `json:"id"`
	Updated  bool   `json:"updated"`
	Unlocked bool   `json:"unlocked"`
	Cleared  bool   `json:"cleared"`
	Noop     bool   `json:"noop"`
	Message  string `json:"message"`
}

type customerBulkResult struct {
	Updated  int                `json:"updated"`
	Skipped  int                `json:"skipped"`
	Unlocked int                `json:"unlocked"`
	Cleared  int                `json:"cleared"`
	Noop     int                `json:"noop"`
	Items    []customerBulkItem `json:"items"`
}

// customerBulkBody 绑定固定请求形状；请求归一和校验由 DTO 完成。
func customerBulkBody[T interface{ CustomerBulkValues() ([]uint64, int, error) }](c *gin.Context) ([]uint64, int, bool) {
	req, err := binding.ParseJSON[T](c)
	if err != nil {
		customerBulkRequestError(c, err)
		return nil, 0, false
	}
	ids, status, err := (*req).CustomerBulkValues()
	if err != nil {
		customerBulkRequestError(c, err)
		return nil, 0, false
	}
	return ids, status, true
}

// customerBulkRequestError 统一输出既有请求错误；数量与上限来自 DTO 返回的错误事实。
func customerBulkRequestError(c *gin.Context, err error) {
	var limit *bulkids.IDsError
	if errors.As(err, &limit) {
		response.ErrorWithMessage(c, 400, fmt.Sprintf(i18n.Translate(bulkids.MsgTooMany, bulkids.TooManyTemplate, response.RequestLanguage(c)), strconv.Itoa(limit.Max), strconv.Itoa(limit.Count)))
		return
	}
	key := userenums.ErrInvalidParam
	if fact, ok := facing.FactOf(err); ok && fact.Key == userenums.ErrCustomerStatusInvalid {
		key = userenums.ErrCustomerStatusInvalid
	}
	response.ErrorWithMessage(c, 400, key)
}

// customerAPIErrorText 逐条写操作的错误文案（JSON 出口，必须交成品文本）。
//
// 判定 / 归口 / 失败日志三件事走共享的 adminui（判据是 pkg/response.IsBusinessError
// 的**形态**）：本模块原先自留的白名单 customerFacingText 已删除 —— 它是第二份真相。
// 场景名用下划线形态的 "user_customer_write"，保住「这是哪条写路径」的日志粒度：
// 点分形态（如 "user.customer.write"）会被 check-controller-i18n-literals.sh 的**形态**
// 判据当成词条 key 字面量统计，而它不是词条 key —— 撞的是门禁正则的假阳性，
// 不是场景名本身有问题，所以只换分隔符、不并成一个更粗的名字。
func customerAPIErrorText(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	return adminui.PageErrorText(c, "user_customer_write", err)
}

// customerAPIWriteError 把一条写错误落成 JSON 响应（业务错误 400，其余 500）。
func customerAPIWriteError(c *gin.Context, err error) {
	code := http.StatusBadRequest
	if !response.IsBusinessError(err) {
		code = http.StatusInternalServerError
	}
	response.ErrorWithMessage(c, code, customerAPIErrorText(c, err))
}

func customerUnlockMessage(res *userdto.CustomerUnlockResp) string {
	if res.Unlocked {
		return userenums.MsgCustomerUnlocked
	}
	if res.Cleared {
		return userenums.MsgCustomerFailuresCleared
	}
	return userenums.MsgCustomerNotLocked
}

// BulkCustomerStatus preserves the single-customer service boundary per item.
func (h *CustomerHandle) BulkCustomerStatus(c *gin.Context) {
	ids, status, ok := customerBulkBody[userdto.CustomerBulkStatusReq](c)
	if !ok {
		return
	}
	result := customerBulkResult{Items: make([]customerBulkItem, 0, len(ids))}
	for _, id := range ids {
		item := customerBulkItem{ID: id}
		res, err := h.svc.SetCustomerStatus(c.Request.Context(), &userdto.CustomerStatusReq{CustomerID: id, Status: status})
		if err != nil {
			item.Message = customerAPIErrorText(c, err)
			result.Skipped++
		} else if res == nil {
			item.Message = response.TranslateMessage(c, userenums.ErrInternal)
			result.Skipped++
		} else {
			item.Updated = true
			result.Updated++
			item.Message = response.TranslateMessage(c, customerStatusMessage(res.Status))
		}
		result.Items = append(result.Items, item)
	}
	response.Success(c, result)
}

// BulkCustomerUnlock reports no-op separately from changed login state and failures.
func (h *CustomerHandle) BulkCustomerUnlock(c *gin.Context) {
	ids, _, ok := customerBulkBody[userdto.CustomerBulkUnlockReq](c)
	if !ok {
		return
	}
	result := customerBulkResult{Items: make([]customerBulkItem, 0, len(ids))}
	for _, id := range ids {
		item := customerBulkItem{ID: id}
		res, err := h.svc.UnlockCustomer(c.Request.Context(), &userdto.CustomerUnlockReq{CustomerID: id})
		if err != nil {
			item.Message = customerAPIErrorText(c, err)
			result.Skipped++
		} else if res == nil {
			item.Message = response.TranslateMessage(c, userenums.ErrInternal)
			result.Skipped++
		} else {
			item.Unlocked = res.Unlocked
			item.Cleared = res.Cleared
			item.Noop = !res.Unlocked && !res.Cleared
			item.Updated = res.Unlocked || res.Cleared
			if item.Unlocked {
				result.Unlocked++
			}
			if item.Cleared {
				result.Cleared++
			}
			if item.Noop {
				result.Noop++
			}
			if item.Updated {
				result.Updated++
			}
			item.Message = response.TranslateMessage(c, customerUnlockMessage(res))
		}
		result.Items = append(result.Items, item)
	}
	summary := i18n.FillTranslate(i18n.TranslateFunc(response.RequestLanguage(c)), userenums.MsgCustomerBulkUnlock, "已解除锁定 {unlocked} 个，已清零失败计数 {cleared} 个，{noop} 个本来就未锁定，{skipped} 个未处理。", map[string]string{"unlocked": strconv.Itoa(result.Unlocked), "cleared": strconv.Itoa(result.Cleared), "noop": strconv.Itoa(result.Noop), "skipped": strconv.Itoa(result.Skipped)})
	response.SuccessWithMessage(c, summary, result)
}
