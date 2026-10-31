package projecthttp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const migrationsDir = "../../../../../public/migrations"

func TestProjectWriteTextKeysAreRegisteredInMigrations(t *testing.T) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("读取迁移目录失败（路径假设变了要同步改）: %v", err)
	}
	var corpus strings.Builder
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(migrationsDir, e.Name()))
		if rerr != nil {
			t.Fatalf("读 %s 失败: %v", e.Name(), rerr)
		}
		corpus.Write(b)
		corpus.WriteString("\n")
	}
	all := corpus.String()
	if len(all) == 0 {
		t.Fatal("迁移语料为空：路径写错时这条测试会变成永远通过的空检查")
	}
	for _, key := range projectWriteTextKeys {
		if !strings.Contains(all, "'"+key+"'") {
			t.Errorf("文案 key %q 没有出现在任何迁移 SQL 里 —— 页面会直接显示这个裸 key（新增词条要写迁移）", key)
		}
	}
}

func TestProjectJumpBackPathsKeepFilterContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name string
		url  string
		got  func(*gin.Context) string
		want string
	}{
		{"主题列表保留工程筛选", "/admin/themes/delete?project=p1&id=t1", themesBack, "/admin/themes?project=p1"},
		{"主题列表无工程时只回路径", "/admin/themes/create", themesBack, "/admin/themes"},
		{"站点设置保留工程", "/admin/settings/save?project=p1", siteSettingsBack, "/admin/settings?project=p1"},
		{"主题设置保留 id", "/admin/themes/settings/save?id=t1", themeSettingsBack, "/admin/themes/settings?id=t1"},
		{"主题设置缺 id 时只回路径", "/admin/themes/settings/save", themeSettingsBack, "/admin/themes/settings"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.url, nil)
			if got := tc.got(c); got != tc.want {
				t.Fatalf("回跳地址 = %q，期望 %q", got, tc.want)
			}
		})
	}
}
