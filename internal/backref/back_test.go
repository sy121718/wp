package backref

import (
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	"go_wp/pkg/backpath"
	"go_wp/pkg/logger"
	"go_wp/pkg/safepath"
)

const (
	warnUnsafePath   = "回跳路径不合法（非站内相对路径或超长），回落调用方给定路径"
	warnDroppedValue = "回跳参数被丢弃（超长或含控制字符）"
)

type logLine struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Path    string `json:"path"`
	Key     string `json:"key"`
	Len     int    `json:"len"`
}

func withTempLogDir(t *testing.T) (string, func() []logLine) {
	t.Helper()
	dir := t.TempDir()
	cfg := viper.New()
	cfg.Set("log.base_dir", dir)

	cfg.Set("log.level", "debug")
	if err := logger.Init(cfg); err != nil {
		t.Fatalf("初始化 logger 失败: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	return dir, func() []logLine {
		files, err := filepath.Glob(filepath.Join(dir, "shell", "app-*.log"))
		if err != nil {
			t.Fatalf("枚举日志文件失败: %v", err)
		}
		var lines []logLine
		for _, f := range files {
			raw, rerr := os.ReadFile(f)
			if rerr != nil {
				t.Fatalf("读取日志文件 %s 失败: %v", f, rerr)
			}
			for _, ln := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				if strings.TrimSpace(ln) == "" {
					continue
				}
				var rec logLine
				if uerr := json.Unmarshal([]byte(ln), &rec); uerr != nil {
					t.Fatalf("日志行不是 JSON（%v）：%s", uerr, ln)
				}
				lines = append(lines, rec)
			}
		}
		return lines
	}
}

func backCtx(t *testing.T, target string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, target, nil)
	return c
}

func TestBackPathOnlyWhitelistedKeys(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create?project=p1&status=enabled&evil=1&keyword=k")
	got := BackPath(c, "/admin/coupons", "project", "status")
	if want := "/admin/coupons?project=p1&status=enabled"; got != want {
		t.Fatalf("期望 %q，实际 %q", want, got)
	}
	if strings.Contains(got, "evil") {
		t.Fatalf("未列出的键被透传：%q", got)
	}
	if lines := readLogs(); len(lines) != 0 {
		t.Fatalf("合法值不应产生日志，实际 %d 条：%+v", len(lines), lines)
	}
}

func TestBackPathDropsEmptyValuesSilently(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create?project=p1&status=&keyword=%20%20")
	got := BackPath(c, "/admin/coupons", "project", "status", "keyword")
	if want := "/admin/coupons?project=p1"; got != want {
		t.Fatalf("期望只剩 project，实际 %q", got)
	}
	if lines := readLogs(); len(lines) != 0 {
		t.Fatalf("空白值必须静默丢弃（不该刷日志），实际 %d 条：%+v", len(lines), lines)
	}
}

func TestBackPathNoParams(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create")
	if got := BackPath(c, "/admin/coupons", "project"); got != "/admin/coupons" {
		t.Fatalf("期望 %q，实际 %q", "/admin/coupons", got)
	}
	if lines := readLogs(); len(lines) != 0 {
		t.Fatalf("无参数不应产生日志，实际 %d 条：%+v", len(lines), lines)
	}
}

func TestBackPathEscapesValues(t *testing.T) {
	_, _ = withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create?keyword="+`a%26b%3Dc`)
	got := BackPath(c, "/admin/coupons", "keyword")
	if want := "/admin/coupons?keyword=a%26b%3Dc"; got != want {
		t.Fatalf("期望转义后的值 %q，实际 %q", want, got)
	}
}

func TestBackPathStableOrder(t *testing.T) {
	_, _ = withTempLogDir(t)
	a := backCtx(t, "/admin/coupons/create?status=enabled&project=p1&page=2")
	b := backCtx(t, "/admin/coupons/create?page=2&project=p1&status=enabled")
	ka := BackPath(a, "/admin/coupons", "project", "status", "page")
	kb := BackPath(b, "/admin/coupons", "project", "status", "page")
	if ka != kb {
		t.Fatalf("同一上下文拼出不同 URL：%q vs %q", ka, kb)
	}
	if want := "/admin/coupons?page=2&project=p1&status=enabled"; ka != want {
		t.Fatalf("期望按键排序 %q，实际 %q", want, ka)
	}
}

func TestBackPathFirstValueWinsForDuplicateKey(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create?project=p1&project=p2")
	if got, want := BackPath(c, "/admin/coupons", "project"), "/admin/coupons?project=p1"; got != want {
		t.Fatalf("期望取第一个值 %q，实际 %q", want, got)
	}
	if lines := readLogs(); len(lines) != 0 {
		t.Fatalf("重复键不是丢弃，不应产生日志，实际 %d 条：%+v", len(lines), lines)
	}
}

func TestBackPathBoundaryValues(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	value := strings.Repeat("k", backpath.MaxQueryValueBytes)
	c := backCtx(t, "/admin/coupons/create?keyword="+value)
	want := "/admin/coupons?keyword=" + value
	if got := BackPath(c, "/admin/coupons", "keyword"); got != want {
		t.Fatalf("恰好 %d 字节的值应当保留，实际 %q", backpath.MaxQueryValueBytes, got)
	}

	path := "/" + strings.Repeat("a", safepath.MaxReturnPathBytes-1)
	plain := backCtx(t, "/admin/coupons/create")
	if got := BackPath(plain, path, "keyword"); got != path {
		t.Fatalf("恰好 %d 字节的路径应当放行，实际 %q", safepath.MaxReturnPathBytes, got)
	}
	if lines := readLogs(); len(lines) != 0 {
		t.Fatalf("边界内的值不应产生日志，实际 %d 条：%+v", len(lines), lines)
	}
}

func TestBackPathRejectsUnsafePath(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create?project=p1")

	badPaths := []string{
		"//evil.example.com/x",
		"https://evil.example.com",
		"admin/coupons",
		"/admin/x\\..\\evil",
		"/admin/x\nLocation: /y",
		"/" + strings.Repeat("a", safepath.MaxReturnPathBytes),
	}
	for i, bad := range badPaths {
		got := BackPath(c, bad, "project")
		if got != adminHomePath {
			t.Fatalf("路径 %q 应当被拒绝并回 %q，实际 %q", bad, adminHomePath, got)
		}
		lines := readLogs()
		if len(lines) != i+1 {
			t.Fatalf("路径 %q 应当恰好新增 1 条日志，实际总数 %d", bad, len(lines))
		}
		last := lines[len(lines)-1]
		if last.Level != "warn" {
			t.Fatalf("路径不合法必须是 warn 级别，实际 %q", last.Level)
		}
		if last.Message != warnUnsafePath {
			t.Fatalf("路径不合法文案漂移：\n got %q\nwant %q", last.Message, warnUnsafePath)
		}
		if last.Path != bad {
			t.Fatalf("日志 path 字段应当是调用点的字面量，got %q want %q", last.Path, bad)
		}
	}
}

func TestBackPathDropsOverlongValueLogsKeyAndLen(t *testing.T) {
	_, readLogs := withTempLogDir(t)

	long := "%20%20" + strings.Repeat("k", backpath.MaxQueryValueBytes+1)
	c := backCtx(t, "/admin/coupons/create?project=p1&keyword="+long)
	if got, want := BackPath(c, "/admin/coupons", "project", "keyword"), "/admin/coupons?project=p1"; got != want {
		t.Fatalf("超长 keyword 应当被丢弃，实际 %q", got)
	}

	lines := readLogs()
	if len(lines) != 1 {
		t.Fatalf("应当恰好 1 条丢弃日志，实际 %d 条：%+v", len(lines), lines)
	}
	if lines[0].Level != "warn" {
		t.Fatalf("丢弃值必须是 warn 级别，实际 %q", lines[0].Level)
	}
	if lines[0].Message != warnDroppedValue {
		t.Fatalf("丢弃值文案漂移：\n got %q\nwant %q", lines[0].Message, warnDroppedValue)
	}
	if lines[0].Key != "keyword" {
		t.Fatalf("日志 key 字段应当是键名，实际 %q", lines[0].Key)
	}
	if want := backpath.MaxQueryValueBytes + 1; lines[0].Len != want {
		t.Fatalf("日志 len 应当是 **trim 后** 的字节数 %d，实际 %d", want, lines[0].Len)
	}
}

func TestBackPathDropsControlCharsLogsKeyAndLen(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create?keyword=a%0Ab&project=p1")
	if got, want := BackPath(c, "/admin/coupons", "project", "keyword"), "/admin/coupons?project=p1"; got != want {
		t.Fatalf("含控制字符的 keyword 应当被丢弃，实际 %q", got)
	}

	lines := readLogs()
	if len(lines) != 1 {
		t.Fatalf("应当恰好 1 条丢弃日志，实际 %d 条：%+v", len(lines), lines)
	}
	if lines[0].Level != "warn" || lines[0].Message != warnDroppedValue {
		t.Fatalf("丢弃日志级别/文案不符：%+v", lines[0])
	}
	if lines[0].Key != "keyword" {
		t.Fatalf("日志 key 字段应当是键名，实际 %q", lines[0].Key)
	}
	if lines[0].Len != len("a\nb") {
		t.Fatalf("日志 len 应当是 trim 后字节数 %d，实际 %d", len("a\nb"), lines[0].Len)
	}
}

func TestBackPathKeepsValidValueSilentInLog(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create?project=p1&status=enabled&keyword=%E4%B8%AD%E6%96%87")
	BackPath(c, "/admin/coupons", "project", "status", "keyword")
	if lines := readLogs(); len(lines) != 0 {
		t.Fatalf("合法值（含多字节中文）不应产生日志，实际 %d 条：%+v", len(lines), lines)
	}
}

func TestBackPathMissingRequestReturnsAdminHomeWithoutLog(t *testing.T) {
	_, readLogs := withTempLogDir(t)

	noRequest, _ := gin.CreateTestContext(httptest.NewRecorder())
	noURL, _ := gin.CreateTestContext(httptest.NewRecorder())
	noURL.Request = &http.Request{}

	cases := []struct {
		name string
		ctx  *gin.Context
	}{
		{"nil 上下文", nil},
		{"nil Request", noRequest},
		{"nil URL", noURL},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BackPath(tc.ctx, "/admin/coupons", "project"); got != adminHomePath {
				t.Fatalf("请求缺失时应当回 %q，实际 %q", adminHomePath, got)
			}
		})
	}
	if lines := readLogs(); len(lines) != 0 {
		t.Fatalf("请求缺失属调用方错误、不是值丢弃，不应刷日志，实际 %d 条：%+v", len(lines), lines)
	}
}

func TestBackPathLogsUnderShellSceneDir(t *testing.T) {
	dir, readLogs := withTempLogDir(t)
	BackPath(backCtx(t, "/admin/coupons/create"), "//evil.example.com/x")

	files, err := filepath.Glob(filepath.Join(dir, "shell", "app-*.log"))
	if err != nil {
		t.Fatalf("枚举日志文件失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("scene 必须固定为 shell（日志目录 %s 下没有文件）", filepath.Join(dir, "shell"))
	}
	if lines := readLogs(); len(lines) != 1 {
		t.Fatalf("应当恰好 1 条日志，实际 %d 条：%+v", len(lines), lines)
	}
}
