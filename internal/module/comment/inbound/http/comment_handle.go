package commenthttp

import (
	"net/http"
	"strconv"
	"strings"

	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/facing"

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

func (h *Handle) Review(c *gin.Context) {
	req, ok := binding.JSON[commentdto.ReviewReq](c)
	if !ok {
		return
	}
	req.ReviewerID = builtin.CurrentUserID(c)
	res, err := h.svc.Review(c.Request.Context(), req)
	if err != nil {
		commentFail(c, err)
		return
	}

	c.JSON(http.StatusOK, response.Response{
		Code: http.StatusOK, Message: commentReviewMessage(c, res), Data: res,
	})
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

func commentReviewMessage(c *gin.Context, res *commentdto.ReviewResp) string {
	pair := commentenums.DoneTemplate(res != nil && res.Status == commentenums.StatusApproved)
	tpl := i18n.TranslateFunc(response.RequestLanguage(c))(pair.Key, pair.Fallback)
	if strings.Count(tpl, "%d") != 1 || strings.Contains(strings.Replace(tpl, "%d", "", 1), "%") {
		tpl = pair.Fallback
	}
	var affected int64
	if res != nil {
		affected = res.Affected
	}
	return strings.Replace(tpl, "%d", strconv.FormatInt(affected, 10), 1)
}
