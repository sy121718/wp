package navigationhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

type Handle struct {
	svc navigationcontract.NavigationService

	writerSvc navigationcontract.TranslationWriteService
}

func NewHandle(svc navigationcontract.NavigationService) *Handle {
	return &Handle{svc: svc}
}

func (h *Handle) SetTranslationWriteService(s navigationcontract.TranslationWriteService) {
	h.writerSvc = s
}

func (h *Handle) Create(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	// 普通界面提交是标准表单；「从来源批量加入」是另一个端点（CreateFromSource）。
	req, err := binding.ParseForm[navigationdto.CreateReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	if _, err := h.svc.Create(c.Request.Context(), req); err != nil {
		pageview.FailStatus(c, navigationErrorStatus(err), navigationErrScene, err)
		return
	}
	pageview.Success(c)
}

// Update 是导航项基本字段的编辑：标准表单，一份确定的输入协议。
// 换位走 /api/navigation/move，译文批量写走 /api/navigation/translations。
func (h *Handle) Update(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[navigationdto.UpdateReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	if _, err := h.svc.Update(c.Request.Context(), req); err != nil {
		pageview.FailStatus(c, navigationErrorStatus(err), navigationErrScene, err)
		return
	}
	pageview.Success(c)
}

// Move 是上移 / 下移：标准表单（id / dir / projectId）。
func (h *Handle) Move(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	req, err := binding.ParseForm[navigationdto.SwapMoveReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	if _, err := h.svc.SwapMove(c.Request.Context(), req); err != nil {
		pageview.FailStatus(c, navigationErrorStatus(err), navigationErrScene, err)
		return
	}
	// 边界移动是合法结果（没有可交换的邻居），按钮在模板侧已按首末项禁用；
	// 成功只需公共提示，不另造结果模板或导航协议。
	pageview.Success(c)
}

// UpdateTranslations 是译文批量的明确 JSON 输入（行是 context/hash/target 三元组）。
func (h *Handle) UpdateTranslations(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	env, err := binding.ParseJSON[navigationdto.UpdateEnvelope](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	if err := env.Validate(); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	h.updateTranslations(c, env)
}

func (h *Handle) updateTranslations(c *gin.Context, env *navigationdto.UpdateEnvelope) {
	req, err := env.DecodeTranslation()
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	if h.writerSvc == nil {
		logger.Scene("navigation").Warn("导航译文写服务未装配，拒绝译文保存")
		pageview.FailScene(c, navigationErrScene, navigationenums.ErrTranslationWriteUnavailableError())
		return
	}
	rows := make([]navigationcontract.TranslationWriteRow, 0, len(req.Rows))
	for _, row := range req.Rows {
		rows = append(rows, navigationcontract.TranslationWriteRow{
			Context: row.Context, Hash: row.Hash, Target: row.Target,
		})
	}
	result, err := h.writerSvc.WriteTranslations(c.Request.Context(), &navigationcontract.TranslationWriteReq{
		ProjectID: req.ProjectID, Lang: req.Lang, Rows: rows,
	})
	if err != nil {
		pageview.FailScene(c, navigationErrScene, err)
		return
	}
	if result == nil {
		logger.Scene("navigation").Warn("导航译文写服务返回空回执，按失败处理")
		pageview.FailScene(c, navigationErrScene, navigationenums.ErrTranslationWriteUnavailableError())
		return
	}

	// 纯成功只给公共一句提示；写入条数是明细，不进入成功消息。
	pageview.Success(c)
}

func (h *Handle) Get(c *gin.Context) {
	req, err := binding.ParseQuery[navigationdto.GetReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgDetailSuccess, res)
}

func (h *Handle) List(c *gin.Context) {
	req, err := binding.ParseQuery[navigationdto.ListReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgListSuccess, list)
}

func (h *Handle) Delete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	// 一个端点一种形状：单条用 id，批量是独立端点 DeleteBulk。
	// 单条分支用显式表单入口：字段与 DeleteReq 的 form tag 一一对应，形状由 binding 判。
	formReq, err := binding.ParseForm[navigationdto.DeleteReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return
	}
	if err := h.svc.Delete(c.Request.Context(), formReq); err != nil {
		pageview.FailStatus(c, navigationErrorStatus(err), navigationErrScene, err)
		return
	}
	pageview.Success(c)
}

func navigationErrorStatus(err error) int {
	// 错误自己声明的状态（批量拒绝、超限、带状态包装）优先：它比按键猜更准确。
	if status, ok := facing.StatusOf(err); ok {
		return status
	}
	fact, business := navigationFact(err)
	if !business {
		return http.StatusInternalServerError
	}

	key, _, _ := strings.Cut(fact.Key, "|")
	// 状态只在 enums 声明一次；这里沿用同一份声明，不再维护第二份 switch。
	return navigationenums.StatusFor(key)
}
