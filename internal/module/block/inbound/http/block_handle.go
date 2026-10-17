package blockhttp

// block_handle.go — 全局块 REST API 的 handler 与错误映射（JSON 模式）。
//
// 注册落点在 block_router.go；页面侧（/admin/blocks）在 block_page.go 与 block_page_router.go。

import (
	"errors"
	"fmt"
	"net/http"

	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockservice "go_wp/internal/module/block/service"
	"go_wp/pkg/binding"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle 全局块 HTTP 处理器。
type Handle struct {
	svc blockcontract.BlockService
}

// List 列出工程全局块（?projectId=&kind=&category=&reuseMode=）。
func (h *Handle) List(c *gin.Context) {
	res, err := h.svc.List(c.Request.Context(), &blockdto.ListReq{
		ProjectID: c.Query("projectId"), Kind: c.Query("kind"), Category: c.Query("category"),
		ReuseMode: c.Query("reuseMode"),
	})
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, res)
}

// Detail 块详情。
func (h *Handle) Detail(c *gin.Context) {
	req, ok := binding.Query[blockdto.DetailReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, res)
}

// Create 新建块。
func (h *Handle) Create(c *gin.Context) {
	req, ok := binding.JSON[blockdto.CreateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, res)
}

// Update 更新块。
func (h *Handle) Update(c *gin.Context) {
	req, ok := binding.JSON[blockdto.UpdateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, res)
}

// Delete 删除块（global 块被引用默认拒绝，force=true 强制删除）。
//
// 被拒（409）时用 blockRefErrorMessage：与批量删除逐条原因同一套文案 ——
// 「哪些页面/资源挡住了」是操作者唯一能据此行动的信息，不能只剩一句「不能删除」。
func (h *Handle) Delete(c *gin.Context) {
	req, ok := binding.JSON[blockdto.DeleteReq](c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockRefErrorMessage(c, err))
		return
	}
	response.Success(c, nil)
}

// BulkDelete 批量删除块（POST /api/block/bulk-delete）。
//
// 为什么要它在服务端，而不是「前端循环调单条删除」：批量 id 是**无界输入**，
// 去重与上限必须落在可信边界内侧 —— 前端那份上限改一行 JS 就没了。
// 这里用 pkg/bulkids.Normalize（终局纯工具），与表单批量（shell.BulkIDs → 同一个函数）
// 共用一份规则：两个入口各复刻一遍，「按去重后条数判上限」这个细节会先分叉。
//
// 语义与旧的页面批量逐字一致：
//   - 逐条走 svc.Delete（与单条删除是同一条路径，因此被引用的 global 块照旧被拒）；
//   - 单条失败**不中断**整批（整批回滚会让用户以为「一个都没删」然后反复重试）；
//   - 部分成功回 200 + 真实计数，不伪装成整批失败。
func (h *Handle) BulkDelete(c *gin.Context) {
	req, ok := binding.JSON[blockdto.BulkDeleteReq](c)
	if !ok {
		return
	}
	ids, nerr := bulkids.Normalize(req.IDs)
	if nerr != nil {
		// 超限：整批拒绝（不静默截断）。文案是 bulkids 的词条 + 带参协议（key|max|count），
		// 由 response 按请求语言取词 —— 与页面批量的超限提示同一词条、同一个数字顺序。
		response.ErrorWithMessage(c, http.StatusBadRequest, bulkTooManyMessage(nerr))
		return
	}
	if len(ids) == 0 {
		// 归一化后一条不剩（提交的全是空白）：不是错误，回空结果，由前端说「没有选中任何块」。
		response.Success(c, &blockdto.BulkDeleteResp{Items: []blockdto.BulkDeleteItemResult{}})
		return
	}

	res := &blockdto.BulkDeleteResp{Items: make([]blockdto.BulkDeleteItemResult, 0, len(ids))}
	for _, id := range ids {
		item := blockdto.BulkDeleteItemResult{ID: id}
		if derr := h.svc.Delete(c.Request.Context(), &blockdto.DeleteReq{ID: id}); derr != nil {
			logger.Scene("block").With("op", "BulkDelete").With("block_id", id).
				With("user_id", builtin.GetUserID(c)).With("path", c.Request.URL.Path).
				Error(derr, "批量删除全局块失败")
			// 逐条原因复用页面删单条时的受控文案 + 引用明细（blockRefErrText）：
			// 两处各写一句会让同一个「为什么删不掉」在不同入口下措辞不同。
			item.Message = blockRefErrText(c, derr)
			res.Skipped++
		} else {
			item.Deleted = true
			res.Deleted++
		}
		res.Items = append(res.Items, item)
	}
	response.Success(c, res)
}

// bulkTooManyMessage 批量超限文案的**带参协议形态**（key|max|count）。
//
// 词条与两个数字都归 pkg/bulkids 所有（bulkids.MsgTooMany 的两个 %s 依次是上限与本次条数，
// 见迁移 277）；模块侧只把它翻成 response 认识的形态，按键取词仍由 pkg/response 做。
// 不要在模块里另拼一句话：去重后的条数只有归一函数算得出来，重算就是第二份真相。
func bulkTooManyMessage(err error) string {
	max, count := bulkids.MaxIDs, 0
	var tooMany *bulkids.IDsError
	if errors.As(err, &tooMany) {
		max, count = tooMany.Max, tooMany.Count
	}
	return fmt.Sprintf("%s|%d|%d", bulkids.MsgTooMany, max, count)
}

// CloneAST 复制块 AST（编辑器「插入-复制」动作）：返回与源块脱钩的独立文档。
func (h *Handle) CloneAST(c *gin.Context) {
	req, ok := binding.JSON[blockdto.CloneReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CloneAST(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, blockErrorStatus(err), blockErrorMessage(err))
		return
	}
	response.Success(c, res)
}

// blockErrorStatus 按 sentinel error 映射 HTTP 状态码（与 page 模块 pageErrorStatus 同构）：
// 参数/校验错误 → 400，资源不存在（块/工程）→ 404，同名冲突 → 409，其余未知错误 → 500。
// 修复前按 err.Error() 中文文案 strings.Contains 匹配（文案改动即失效）；
// 修复后基于 blockservice 包 sentinel error 精确 errors.Is 判定。
func blockErrorStatus(err error) int {
	switch {
	case errors.Is(err, blockservice.ErrNotFound), errors.Is(err, blockservice.ErrProjectNotFound):
		return http.StatusNotFound
	case errors.Is(err, blockservice.ErrDuplicate):
		return http.StatusConflict
	case errors.Is(err, blockservice.ErrBlockInUse):
		// 409：资源仍被引用，属状态冲突而非参数错误。
		return http.StatusConflict
	case errors.Is(err, blockservice.ErrParamRequired),
		errors.Is(err, blockservice.ErrNameRequired),
		errors.Is(err, blockservice.ErrInvalidDoc),
		errors.Is(err, blockservice.ErrInvalidKind),
		errors.Is(err, blockservice.ErrInvalidCategory),
		errors.Is(err, blockservice.ErrInvalidReuseMode):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// blockErrorMessage 把 service 错误映射为响应消息：
// 已知业务错误（sentinel）其 Error() 即 blockenums 文案，直接下发；
// 未知系统错误（blockErrorStatus 归为 500）改用兜底文案下发，原文只进日志，
// 避免 err.Error() 把内部细节（SQL 错误、连接信息）泄露给客户端。
func blockErrorMessage(err error) string {
	if blockErrorStatus(err) == http.StatusInternalServerError {
		logger.Scene("block").Error(err, "block 接口内部错误")
		return blockenums.MsgInternalError
	}
	return err.Error()
}

// paramBindFail 请求绑定失败的统一出口（400 + 受控文案）。
//
// 不把绑定错误原文拼进响应：gin 的绑定错误会带上 Go 结构体与字段名
// （如 `json: cannot unmarshal string into Go struct field CreateReq.title of type string`），
// 那是实现细节 —— 对外只说「参数不合法」，原文进日志供排障。
//
// 原先这里是 `response.ParamError(c, err.Error())`。它一直待在门禁盲区里：形态 ① 只认
// `*.ErrorWithMessage(` 与 `c.String(`，而 `ParamError` 是同一个包里的同族出口却不在判据里。
// 2026-09 第三批把 ParamError 纳入判据后，本模块这几处立刻被扫出来 —— 判据是**形状**，
// 不是字面量；同族出口漏一个就等于那一族都没管住。
func paramBindFail(c *gin.Context, err error) {
	if err != nil {
		// 绑定失败是客户端输入问题，按 warn 记（不污染错误日志）。
		logger.Scene("block").With("path", c.Request.URL.Path).
			With("detail", err.Error()).Warn("block 接口请求绑定失败")
	}
	response.ParamError(c)
}
