package navigationhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"
)

// bindBulkDeleteForm 绑定批量删除表单；失败已由这里写出 400。
func bindBulkDeleteForm(c *gin.Context) (*navigationdto.BulkDeleteForm, bool) {
	form, err := binding.ParseForm[navigationdto.BulkDeleteForm](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return nil, false
	}
	if len(form.IDs) == 0 {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return nil, false
	}
	return form, true
}

// DeleteBulk 是批量删除的独立端点：标准表单，ids 重复提交。
//
// 控制器只做表单绑定与“至少有一个值”的基础校验；空串、非法 uuid、不存在项和
// 跨工程批量都由 service 统一判定并整批拒绝，控制器不先过滤/去重，避免把非法项
// 悄悄丢掉后变成一次部分提交。
func (h *Handle) DeleteBulk(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, ok := bindBulkDeleteForm(c)
	if !ok {
		return
	}
	if err := h.svc.BulkDelete(c.Request.Context(), &navigationdto.BulkDeleteReq{IDs: form.IDs}); err != nil {
		pageview.FailScene(c, navigationErrScene, err)
		return
	}
	pageview.Success(c)
}
