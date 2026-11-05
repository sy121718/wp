package commenthttp

import (
	"net/http"

	"go_wp/pkg/binding"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/facing"
	pageview "go_wp/pkg/view"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	"go_wp/pkg/response"
)

type Handle struct {
	svc commentcontract.CommentService
}

func NewHandle(svc commentcontract.CommentService) *Handle { return &Handle{svc: svc} }

func (h *Handle) AdminList(c *gin.Context) {
	req, ok := binding.Query[commentdto.AdminListReq](c)
	if !ok {
		return
	}
	res, err := h.svc.AdminList(c.Request.Context(), req)
	if err != nil {
		commentFail(c, err)
		return
	}
	response.Success(c, res)
}

// Review 接收标准表单，成功后由 Jet 返回完整结果句。
func (h *Handle) Review(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var req commentdto.ReviewReq
	if err := c.ShouldBindWith(&req, ginbinding.FormPost); err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, "comment", err)
		return
	}
	req.ReviewerID = builtin.CurrentUserID(c)
	res, err := h.svc.Review(c.Request.Context(), &req)
	if err != nil {
		pageview.FailScene(c, "comment", err)
		return
	}
	c.HTML(http.StatusOK, "admin/comment/comment_review_result.html", pageview.Objects(
		pageview.Object("Affected", reviewAffected(res)),
		pageview.Object("Approved", res != nil && res.Status == commentenums.StatusApproved),
	))
}

func commentFail(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	fact, business := facing.FactOf(err)
	switch {
	case business && fact.Key == commentenums.ErrUnavailable:

		status = http.StatusServiceUnavailable
	case business:
		status = http.StatusBadRequest
	}
	response.ErrorWithMessage(c, status, response.MessageOf(c, "comment", err))
}

func reviewAffected(res *commentdto.ReviewResp) int64 {
	if res == nil {
		return 0
	}
	return res.Affected
}
