package blueprinthttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintdto "go_wp/internal/module/blueprint/dto"
	blueprintenums "go_wp/internal/module/blueprint/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/response"
)

type Handle struct {
	svc blueprintcontract.BlueprintService
}

func NewHandle(svc blueprintcontract.BlueprintService) *Handle {
	return &Handle{svc: svc}
}

func (h *Handle) Create(c *gin.Context) {
	req, err := binding.ParseJSON[blueprintdto.CreateReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgCreateSuccess, res)
}

func (h *Handle) Update(c *gin.Context) {
	req, err := binding.ParseJSON[blueprintdto.UpdateReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgUpdateSuccess, res)
}

func (h *Handle) Publish(c *gin.Context) {
	req, err := binding.ParseJSON[blueprintdto.PublishReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Publish(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgPublishSuccess, res)
}

// Get / List / Init 是 GET 读端点：参数在查询串上，不在 body。
// 写成 ParseForm 会把 GET 永远绑成零值（FormPost 只读 body），带 required 的 id
// 直接 400、列表则静默返回空集；这里用显式查询入口声明唯一输入协议。
func (h *Handle) Get(c *gin.Context) {
	req, err := binding.ParseQuery[blueprintdto.GetReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusNotFound, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgDetailSuccess, res)
}

func (h *Handle) List(c *gin.Context) {
	req, err := binding.ParseQuery[blueprintdto.ListReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgListSuccess, list)
}

func (h *Handle) Delete(c *gin.Context) {
	req, err := binding.ParseJSON[blueprintdto.DeleteReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.SuccessWithMessage(c, blueprintenums.MsgDeleteSuccess, nil)
}

func (h *Handle) Init(c *gin.Context) {
	req, err := binding.ParseQuery[blueprintdto.GetReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	doc, err := h.svc.InitPageDocument(c.Request.Context(), req.ID)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "blueprint", err)
		return
	}
	response.Success(c, doc)
}
