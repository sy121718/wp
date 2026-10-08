package projecthttp

// project_err_test.go — 项目域页面错误归口的判据守卫（就近单测，不碰数据库）。
//
// 守一件事，且是「错了会静默」的那种：判定表把哨兵映射到哪一对 (状态码, key) ——
// 映射错了就是「用户能自己修的问题被说成系统故障」。
//
// 读侧（?err= 的受控文案集合与判定）已随写动作出口改造整批删除：结论改由
// shell.RenderJump 渲染整页提示（见 project_jump.go），不再经查询参数回带，
// 所以「写侧产物必须被读侧白名单覆盖」那条对账不再需要 —— 换成「写侧 key 必须在
// 迁移里登记」（project_err_i18n_test.go 的 projectWriteTextKeys 对账）。

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	projectenums "go_wp/internal/module/project/enums"
	service "go_wp/internal/module/project/service"
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

// TestProjectWriteTextKeysCoverStatusTextOutputs 写侧 key 登记表必须覆盖判定表的**全部**产物。
//
// 判定表产物（业务文案 key）会被渲染到提示页上，漏登记的表现是「页面显示裸 key」——
// 不报错、不记日志，只有人眼能发现（真正的「必须在迁移里登记」由
// project_err_i18n_test.go 的 projectWriteTextKeys 对账钉住，这里先保证登记表本身完整）。
func TestProjectWriteTextKeysCoverStatusTextOutputs(t *testing.T) {
	sentinels := []error{
		service.ErrThemeNotFound, service.ErrProjectNotFound,
		service.ErrThemeIsActive, service.ErrThemeNameRequired,
		service.ErrThemeDuplicateName, service.ErrThemeProjectIDEmpty,
		service.ErrInvalidThemeSettings, service.ErrThemeProjectRequired,
		service.ErrInvalidName, service.ErrInvalidSettings, service.ErrInvalidParam,
		service.ErrLocaleNoTranslations,
	}
	registered := make(map[string]bool, len(projectWriteTextKeys))
	for _, k := range projectWriteTextKeys {
		registered[k] = true
	}
	// 两个域的归口文案也在登记表里（default 分支落到哪一个取决于调用方）。
	for _, fallback := range []string{projectenums.ErrThemeInternal, projectenums.ErrProjectInternal} {
		if !registered[fallback] {
			t.Errorf("归口文案 %q 不在 projectWriteTextKeys 里：装载失败降级渲染的提示会显示裸 key", fallback)
		}
		_, key := projectErrStatusText(errors.New("boom"), fallback)
		if !registered[key] {
			t.Errorf("判定表产物 %q（fallback=%s）不在 projectWriteTextKeys 里", key, fallback)
		}
	}
	for _, err := range sentinels {
		for _, fallback := range []string{projectenums.ErrThemeInternal, projectenums.ErrProjectInternal} {
			_, key := projectErrStatusText(err, fallback)
			if !registered[key] {
				t.Errorf("判定表产物 %q（err=%v）不在 projectWriteTextKeys 里", key, err)
			}
		}
	}
}
