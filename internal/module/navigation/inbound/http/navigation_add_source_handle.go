package navigationhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/binding"
	pageview "go_wp/pkg/view"
)

// CreateFromSource 是「从页面/内容等来源批量加入菜单」的独立端点：
// 一个端点一种确定协议，输入是标准表单（projectId / kind / sourceType / ids）。
//
// 控制器只做表单绑定与基础必填校验，然后把原始 ids 整批交给 service：
// 空标识、非法 uuid、超限、候选缺失、无公开路径与路径冲突都由 service 一次性判定，
// 控制器不先过滤/去重，避免把「混合空 ID」悄悄变成一次合法的部分提交。
// 成功/失败都走公共出口：成功只播报一句，失败按真实状态码渲染共享错误片段。
// bindAddSourceForm 应用请求体上限并绑定表单；失败已由这里写出 400。
//
// 批量 ids 可能很长，超限整批拒绝而不是部分执行。
func bindAddSourceForm(c *gin.Context) (*navigationdto.AddSourceForm, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, navigationWriteMaxBodyBytes)
	form, err := binding.ParseForm[navigationdto.AddSourceForm](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, navigationErrScene, navigationenums.Err(navigationenums.ErrInvalidParam))
		return nil, false
	}
	return form, true
}

func (h *Handle) CreateFromSource(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	form, ok := bindAddSourceForm(c)
	if !ok {
		return
	}
	if _, err := h.svc.AddSources(c.Request.Context(), form.Req()); err != nil {
		pageview.FailScene(c, navigationErrScene, err)
		return
	}
	pageview.Success(c)
}
