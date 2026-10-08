package projecthttp

// project_err_i18n_test.go — 页面提示文案的**词条登记**对账（静态检查，不连库）。
//
// 为什么单独守这一条：写动作的结论由 shell.RenderJump 渲染整页提示，Msg 是
// 直接渲染的文本（不经 response 的 translate），而「键存在但词条没登记」的表现是
// **页面显示裸 key**（如 `ErrThemeIDRequired` 直接摆给运营看）——不报错、不记日志，
// 只有人眼能发现。实测踩过一次同族问题：`c.String(500, "MsgInternalError")`
// 渲染出英文 key，整批页面都是这个样。
//
// 判据用**迁移 SQL 里出现过这个 key**而不是「库里有行」：迁移是词条的唯一登记处
// （新增词条必须写迁移，见 AGENTS.md「数据库」），而测试库不跑 seed，
// 连库断言会把「seed 没跑」误判成「词条缺失」。
//
// 它是启发式的：key 出现在注释里也算命中。漏报（注释里提到但没真插）留给
// 「页面显示裸 key」这个一眼可见的症状兜底 —— 静态检查抓的是「完全没登记」。

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// migrationsDir 从本测试文件出发定位 public/migrations。
const migrationsDir = "../../../../../public/migrations"

// TestProjectWriteTextKeysAreRegisteredInMigrations 每个写侧提示 key 都必须在迁移 SQL 里登记。
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

// TestProjectJumpBackPathsKeepFilterContext 回跳地址由 shell.BackPath 从本次请求的 query 读回，
// 按调用点列出的键保留筛选上下文。
//
// 这两点错了的症状都不明显：键表漏一个 → 「写完跳回去筛选静默丢了」（不报错、日志也干净）；
// 回跳地址不收敛成站内相对路径 → 开放重定向（由 shell.BackPath 的判据兜底，另有单测）。
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
