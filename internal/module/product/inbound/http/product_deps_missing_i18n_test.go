package producthttp

import (
	"go_wp/internal/adminui"
	"net/http"
	"strings"
	"testing"
)

// product_deps_missing_i18n_test.go — 详情页模板页「依赖未装配」这条提示的形态。
//
// 出口已从整页提示页改为**原页失败态**：detailTemplateLoadFailureView 给齐模板必需键，
// LoadFailed + LoadError 就地说明「能力未装配」。提示页（shell.RenderJump）随写动作
// 迁到 JSON 基座而删除 —— 装配缺陷不该把人送到另一个页面去读一句话。
//
// 本文件守两件错了会静默的事：
//
//	· 提示文案取词必须非空（取词失败会让页面显示一句空话，用户以为操作成功了）；
//	· 失败视图必须是 500 档 + LoadFailed，否则模板会把它渲染成「数据读不出来」。
func TestProductDepsMissingFacing(t *testing.T) {
	c, _ := newProductJumpContext(t, "", "", "")
	if strings.TrimSpace(detailTemplateDepsMissingMsg.Key) == "" {
		t.Fatal("这条提示必须带 i18n key：写侧只交事实，取词在渲染期")
	}
	if got := strings.TrimSpace(adminui.MsgText(c, detailTemplateDepsMissingMsg)); got == "" {
		t.Fatal("写侧产出了空文案：页面提示不能是空白")
	}
	view := detailTemplateLoadFailureView(http.StatusInternalServerError)
	if !view.LoadFailed || view.HTTPStatus != http.StatusInternalServerError {
		t.Fatalf("依赖未装配应是 500 失败态，实际 LoadFailed=%v status=%d", view.LoadFailed, view.HTTPStatus)
	}
	if view.Title == "" || view.Menu == "" {
		t.Fatal("失败视图必须给齐模板必需键（Title / Menu），否则整页渲染失败")
	}
}
