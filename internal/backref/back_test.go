package backref

// back_test.go — 回跳请求适配（back.go）的判据。
//
// 两类断言**分开**记：
//
//  1. **返回值边界**（与 internal/shell/back_test.go 同口径）：白名单只透传列出的键、
//     路径必须是站内相对路径、值做形状收敛、产物稳定、边界值恰好收/放；
//  2. **结构化日志**：两条 Warn 的级别 / 消息 / 字段（path、key、**trim 后**字节 len），
//     以及「空白值与合法值**不刷日志**」的负断言 —— 负断言是判据的一半：只断言
//     「丢弃时有日志」，把空值也拖进 Warn 分支同样「绿」。
//
// 日志断言把 logger.Init 指向 t.TempDir()（先例：internal/module/admin/inbound/http/
// admin_err_param_test.go），不新增钩子、不读全局日志目录；scene 固定 "shell"，
// 所以日志落在 <dir>/shell/app-*.log —— 该路径本身也是兼容性断言。
//
// **串行隔离**：logger 是进程级单例，本文件所有用例都不 t.Parallel()，
// 每个用例自带 logger.Init + t.Cleanup(logger.Close)，用例之间不共享日志目录。

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

// 两条 Warn 的文案逐字（与 internal/shell 的对应实现一致：迁移动的是位置，不是文案）。
const (
	warnUnsafePath   = "回跳路径不合法（非站内相对路径或超长），回控制面首页"
	warnDroppedValue = "回跳参数被丢弃（超长或含控制字符）"
)

// logLine 结构化日志行（zap production encoder：time/level/message/caller + 自定义字段）。
type logLine struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Path    string `json:"path"`
	Key     string `json:"key"`
	Len     int    `json:"len"`
}

// withTempLogDir 把 logger 指到临时目录，返回该目录与「读取 shell 场景全部日志行」的闭包。
func withTempLogDir(t *testing.T) (string, func() []logLine) {
	t.Helper()
	dir := t.TempDir()
	cfg := viper.New()
	cfg.Set("log.base_dir", dir)
	// debug 级：任何一种「本不该出现」的日志都会落盘，负断言才有意义
	// （若只开 warn，被误降级成 debug 的那条会消失，负断言就变成了假证据）。
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

// TestBackPathOnlyWhitelistedKeys 只有列出的键会被透传，且合法值不刷日志。
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

// TestBackPathDropsEmptyValuesSilently 空白值丢弃且**静默**（不产出噪音、也不刷日志）。
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

// TestBackPathNoParams 没有任何可透传参数时保持路径原样（不带问号）。
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

// TestBackPathEscapesValues 值里的分隔符必须转义（否则 `a&b=c` 会裂成两个参数）。
func TestBackPathEscapesValues(t *testing.T) {
	_, _ = withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create?keyword="+`a%26b%3Dc`)
	got := BackPath(c, "/admin/coupons", "keyword")
	if want := "/admin/coupons?keyword=a%26b%3Dc"; got != want {
		t.Fatalf("期望转义后的值 %q，实际 %q", want, got)
	}
}

// TestBackPathStableOrder 产物稳定：参数顺序不影响结果（url.Values.Encode 按键排序）。
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

// TestBackPathFirstValueWinsForDuplicateKey 同键重复出现时取第一个（url.Values.Get 语义）。
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

// TestBackPathBoundaryValues 边界值恰好收/放：值 == MaxQueryValueBytes 保留、
// 路径长度 == MaxReturnPathBytes 放行（差 1 字节才拒绝）。
func TestBackPathBoundaryValues(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	value := strings.Repeat("k", backpath.MaxQueryValueBytes)
	c := backCtx(t, "/admin/coupons/create?keyword="+value)
	want := "/admin/coupons?keyword=" + value
	if got := BackPath(c, "/admin/coupons", "keyword"); got != want {
		t.Fatalf("恰好 %d 字节的值应当保留，实际 %q", backpath.MaxQueryValueBytes, got)
	}

	// 路径边界用**不带参数**的请求：同一个上下文里 keyword 还在，断言会变成「路径 + 参数」。
	path := "/" + strings.Repeat("a", safepath.MaxReturnPathBytes-1)
	plain := backCtx(t, "/admin/coupons/create")
	if got := BackPath(plain, path, "keyword"); got != path {
		t.Fatalf("恰好 %d 字节的路径应当放行，实际 %q", safepath.MaxReturnPathBytes, got)
	}
	if lines := readLogs(); len(lines) != 0 {
		t.Fatalf("边界内的值不应产生日志，实际 %d 条：%+v", len(lines), lines)
	}
}

// TestBackPathRejectsUnsafePath 非站内相对路径/超长路径一律回控制面首页，并逐条记 Warn（带 path）。
func TestBackPathRejectsUnsafePath(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create?project=p1")

	badPaths := []string{
		"//evil.example.com/x",     // 协议相对跳转
		"https://evil.example.com", // 绝对外站地址
		"admin/coupons",            // 不以 / 开头：浏览器会按当前路径拼接
		"/admin/x\\..\\evil",       // 反斜杠（部分客户端当路径分隔符）
		"/admin/x\nLocation: /y",   // 控制字符（响应头注入面）
		"/" + strings.Repeat("a", safepath.MaxReturnPathBytes), // 超长
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

// TestBackPathDropsOverlongValueLogsKeyAndLen 超长值丢弃：其余参数照常，日志带 key 与 trim 后 len。
func TestBackPathDropsOverlongValueLogsKeyAndLen(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	// 原始值 = 2 个空格 + 201 个 k：trim 后 201 字节 —— len 字段若用未 trim 的长度（203）即被抓。
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

// TestBackPathDropsControlCharsLogsKeyAndLen 含控制字符的值丢弃，日志同样带 key 与 trim 后 len。
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

// TestBackPathKeepsValidValueSilentInLog 合法值不产生任何日志（负断言）。
func TestBackPathKeepsValidValueSilentInLog(t *testing.T) {
	_, readLogs := withTempLogDir(t)
	c := backCtx(t, "/admin/coupons/create?project=p1&status=enabled&keyword=%E4%B8%AD%E6%96%87")
	BackPath(c, "/admin/coupons", "project", "status", "keyword")
	if lines := readLogs(); len(lines) != 0 {
		t.Fatalf("合法值（含多字节中文）不应产生日志，实际 %d 条：%+v", len(lines), lines)
	}
}

// TestBackPathMissingRequestReturnsAdminHomeWithoutLog 请求缺失三形态：回首页且不写日志。
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

// TestBackPathLogsUnderShellSceneDir scene 固定 "shell"：日志必须落在 <base>/shell/ 下。
//
// 这条守的是**运维兼容**：scene 就是目录名，改名会让既有检索路径与告警规则静默失效，
// 因此迁移不夹带改名（要改必须单独批准）。
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
