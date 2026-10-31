package blockhttp

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
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

type Handle struct {
	svc blockcontract.BlockService
}

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

func (h *Handle) Create(c *gin.Context) {
	req, ok := binding.JSON[blockdto.CreateReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorStatusAuto(c, "block", err)
		return
	}
	response.Success(c, res)
}

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

func (h *Handle) BulkDelete(c *gin.Context) {
	req, ok := binding.JSON[blockdto.BulkDeleteReq](c)
	if !ok {
		return
	}
	ids, nerr := bulkids.Normalize(req.IDs)
	if nerr != nil {

		response.ErrorWithMessage(c, http.StatusBadRequest, bulkTooManyMessage(nerr))
		return
	}
	if len(ids) == 0 {

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

func bulkTooManyMessage(err error) string {
	max, count := bulkids.MaxIDs, 0
	var tooMany *bulkids.IDsError
	if errors.As(err, &tooMany) {
		max, count = tooMany.Max, tooMany.Count
	}
	return fmt.Sprintf("%s|%d|%d", bulkids.MsgTooMany, max, count)
}

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

func blockErrorStatus(err error) int {

	if status, ok := facing.StatusOf(err); ok {
		return status
	}
	switch {
	case errors.Is(err, blockservice.ErrNotFound), errors.Is(err, blockservice.ErrProjectNotFound):
		return http.StatusNotFound
	case errors.Is(err, blockservice.ErrDuplicate):
		return http.StatusConflict
	case errors.Is(err, blockservice.ErrBlockInUse):

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

func blockErrorMessage(err error) string {
	if blockErrorStatus(err) == http.StatusInternalServerError {
		logger.Scene("block").Error(err, "block 接口内部错误")
		return blockenums.MsgInternalError
	}
	return err.Error()
}

func paramBindFail(c *gin.Context, err error) {
	if err != nil {

		logger.Scene("block").With("path", c.Request.URL.Path).
			With("detail", err.Error()).Warn("block 接口请求绑定失败")
	}
	response.ParamError(c)
}
