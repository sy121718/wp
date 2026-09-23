package projecthttp

// project_err_test.go — 项目域页面错误归口的判据守卫（就近单测，不碰数据库）。
//
// 守两件事，都是「错了会静默」的那种：
//  1. 判定表把哨兵映射到哪一对 (状态码, key) —— 映射错了就是「用户能自己修的问题被说成系统故障」；
//  2. 读侧候选是否覆盖了写侧全部产物 —— 漏一个 key 的症状是「写侧发了提示、页面上什么都不显示」，
//     既不报错也不记日志，只能靠这条对账发现。
//
// 页面级的「装载失败仍渲染完整页」「写失败 303 + ?err=」在
// public/test/project/feature/project_page_err_test.go（需要真实装配与模板渲染）。

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	projectenums "go_wp/internal/module/project/enums"
	service "go_wp/internal/module/project/service"

	"github.com/gin-gonic/gin"
)

// TestProjectErrStatusTextMapsSentinels 判定表逐个哨兵的对账。
func TestProjectErrStatusTextMapsSentinels(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		fallback string
		want     int
		wantKey  string
	}{
		{"主题不存在→404", service.ErrThemeNotFound, projectenums.ErrThemeInternal, http.StatusNotFound, projectenums.ErrThemeNotFound},
		{"工程不存在→404", service.ErrProjectNotFound, projectenums.ErrProjectInternal, http.StatusNotFound, projectenums.ErrProjectNotFound},
		{"激活主题不可删→400", service.ErrThemeIsActive, projectenums.ErrThemeInternal, http.StatusBadRequest, projectenums.ErrThemeIsActive},
		{"主题名为空→400", service.ErrThemeNameRequired, projectenums.ErrThemeInternal, http.StatusBadRequest, projectenums.ErrThemeNameRequired},
		{"主题重名→400", service.ErrThemeDuplicateName, projectenums.ErrThemeInternal, http.StatusBadRequest, projectenums.ErrThemeDuplicateName},
		{"缺工程 ID→400", service.ErrThemeProjectIDEmpty, projectenums.ErrThemeInternal, http.StatusBadRequest, projectenums.ErrThemeProjectIDEmpty},
		{"主题设置非法→400", service.ErrInvalidThemeSettings, projectenums.ErrThemeInternal, http.StatusBadRequest, projectenums.ErrInvalidThemeSettings},
		// ErrThemeProjectRequired 原先落 default（500 + 「主题服务内部错误」）—— 它是业务错误，
		// 说成系统故障会让用户去重试一个永远失败的操作。
		{"需显式工程→400", service.ErrThemeProjectRequired, projectenums.ErrThemeInternal, http.StatusBadRequest, projectenums.ErrProjectRequired},
		{"工程名非法→400", service.ErrInvalidName, projectenums.ErrProjectInternal, http.StatusBadRequest, projectenums.ErrInvalidName},
		{"设置非 JSON→400", service.ErrInvalidSettings, projectenums.ErrProjectInternal, http.StatusBadRequest, projectenums.ErrInvalidSettings},
		{"参数无效→400", service.ErrInvalidParam, projectenums.ErrProjectInternal, http.StatusBadRequest, projectenums.ErrInvalidParam},
		// 基础设施故障与 nil：一律归口到调用方给的 key（两个域各自不同）。
		{"驱动原文→主题归口", errors.New("pq: connection refused"), projectenums.ErrThemeInternal, http.StatusInternalServerError, projectenums.ErrThemeInternal},
		{"驱动原文→设置归口", errors.New("pq: connection refused"), projectenums.ErrProjectInternal, http.StatusInternalServerError, projectenums.ErrProjectInternal},
		{"nil→归口", nil, projectenums.ErrThemeInternal, http.StatusInternalServerError, projectenums.ErrThemeInternal},
		// 包装过的哨兵仍要命中（service 会用 %w 补上下文）。
		{"包装哨兵仍命中", fmt.Errorf("激活主题 %s 失败: %w", "t1", service.ErrThemeIsActive), projectenums.ErrThemeInternal, http.StatusBadRequest, projectenums.ErrThemeIsActive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, key := projectErrStatusText(tc.err, tc.fallback)
			if status != tc.want {
				t.Errorf("状态码 = %d, 期望 %d", status, tc.want)
			}
			if key != tc.wantKey {
				t.Errorf("归口 key = %q, 期望 %q", key, tc.wantKey)
			}
		})
	}
}

// TestProjectPageErrKeysCoverStatusTextOutputs 读侧候选必须覆盖判定表的**全部**产物。
//
// 这是「写侧与读侧两处不同步」的唯一自动守卫：读侧未登记的 key 会被 shell.FacingNotice
// 判成伪造而落空串 —— 写侧明明回带了提示，页面上却什么都没有，且没有任何日志。
func TestProjectPageErrKeysCoverStatusTextOutputs(t *testing.T) {
	sentinels := []error{
		service.ErrThemeNotFound, service.ErrProjectNotFound,
		service.ErrThemeIsActive, service.ErrThemeNameRequired,
		service.ErrThemeDuplicateName, service.ErrThemeProjectIDEmpty,
		service.ErrInvalidThemeSettings, service.ErrThemeProjectRequired,
		service.ErrInvalidName, service.ErrInvalidSettings, service.ErrInvalidParam,
	}
	registered := make(map[string]bool, len(projectPageErrKeys))
	for _, k := range projectPageErrKeys {
		registered[k] = true
	}
	// 两个域的归口文案也在候选里（default 分支落到哪一个取决于调用方）。
	for _, fallback := range []string{projectenums.ErrThemeInternal, projectenums.ErrProjectInternal} {
		if !registered[fallback] {
			t.Errorf("归口文案 %q 不在 projectPageErrKeys 里：装载失败降级渲染的提示会被读侧判成伪造", fallback)
		}
		_, key := projectErrStatusText(errors.New("boom"), fallback)
		if !registered[key] {
			t.Errorf("判定表产物 %q（fallback=%s）不在 projectPageErrKeys 里", key, fallback)
		}
	}
	for _, err := range sentinels {
		for _, fallback := range []string{projectenums.ErrThemeInternal, projectenums.ErrProjectInternal} {
			_, key := projectErrStatusText(err, fallback)
			if !registered[key] {
				t.Errorf("判定表产物 %q（err=%v）不在 projectPageErrKeys 里", key, err)
			}
		}
	}
}

// TestProjectPageErrTextRejectsForgedNotice 读侧整体匹配：手拼的 ?err= 一律落空串。
//
// 判据不是 strings.Contains —— 只要夹带一段已知文案就能塞任意前缀后缀，
// 所以这里特意用「已知 key + 后缀」构造一个应当被拒的串。
func TestProjectPageErrTextRejectsForgedNotice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, raw := range []string{
		"",
		"   ",
		"这条提示是我手写的",
		// 前缀伪造：候选本身可能命中（见下方说明），但夹带后必须是空串
		"系统内部错误，请稍后重试 附带一段伪造内容",
		// 带控制字符：Jet 已做 HTML 转义，但换行能把一行提示拆成两条「系统消息」的观感
		"伪造\n第二行",
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/themes?err="+url.QueryEscape(raw), nil)
		if got := projectPageErrText(c, raw); got != "" {
			t.Errorf("projectPageErrText(%q) = %q, 期望空串（未命中白名单）", raw, got)
		}
	}
}
