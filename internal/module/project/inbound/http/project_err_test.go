package projecthttp

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	projectenums "go_wp/internal/module/project/enums"
	service "go_wp/internal/module/project/service"
)

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

		{"需显式工程→400", service.ErrThemeProjectRequired, projectenums.ErrThemeInternal, http.StatusBadRequest, projectenums.ErrProjectRequired},
		{"工程名非法→400", service.ErrInvalidName, projectenums.ErrProjectInternal, http.StatusBadRequest, projectenums.ErrInvalidName},
		{"设置非 JSON→400", service.ErrInvalidSettings, projectenums.ErrProjectInternal, http.StatusBadRequest, projectenums.ErrInvalidSettings},
		{"参数无效→400", service.ErrInvalidParam, projectenums.ErrProjectInternal, http.StatusBadRequest, projectenums.ErrInvalidParam},

		{"驱动原文→主题归口", errors.New("pq: connection refused"), projectenums.ErrThemeInternal, http.StatusInternalServerError, projectenums.ErrThemeInternal},
		{"驱动原文→设置归口", errors.New("pq: connection refused"), projectenums.ErrProjectInternal, http.StatusInternalServerError, projectenums.ErrProjectInternal},
		{"nil→归口", nil, projectenums.ErrThemeInternal, http.StatusInternalServerError, projectenums.ErrThemeInternal},

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
