package scaffold

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModuleFilesActualFileSet(t *testing.T) {
	const name = "member_card"
	files, err := ModuleFiles(name, "go_wp")
	if err != nil {
		t.Fatalf("ModuleFiles: %v", err)
	}
	want := []string{
		"README.md",
		"contract/" + name + "_contract.go",
		"dto/" + name + "_req.go",
		"dto/" + name + "_resp.go",
		"enums/" + name + "_enums.go",
		"model/" + name + "_model.go",
		"service/" + name + "_service.go",
		"inbound/http/" + name + "_router.go",
		"inbound/http/" + name + "_handle.go",
	}
	if len(files) != len(want) {
		t.Fatalf("生成文件数 = %d，want %d：%v", len(files), len(want), files)
	}
	for _, rel := range want {
		if _, ok := files[rel]; !ok {
			t.Errorf("缺文件 %s", rel)
		}
	}
}

func TestModuleFilesRejectsBadName(t *testing.T) {
	for _, name := range []string{"", "a", "Member", "member-card", "member.card", "member/card", "1member", "member ", "_member"} {
		if _, err := ModuleFiles(name, "go_wp"); err == nil {
			t.Errorf("非法模块名 %q 应拒绝", name)
		}
	}
	for _, name := range []string{"ab", "member", "member_card"} {
		if _, err := ModuleFiles(name, "go_wp"); err != nil {
			t.Errorf("合法模块名 %q 不该被拒绝: %v", name, err)
		}
	}
}

func TestModuleFilesRejectsBadModulePath(t *testing.T) {
	bad := []string{
		"", " ", "go wp", "go_wp ", " go_wp", "go_wp\"", "go_wp`", "go_wp\\x", "go_wp;rm",
		"go_wp/", "/go_wp", "go_wp//x", "example.org/../x", "example.org/./x", "example.org/.x",
		"example.org/x.", "go_wp\nx", "go_wp\x00", strings.Repeat("a", 256),
	}
	for _, path := range bad {
		if _, err := ModuleFiles("member", path); err == nil {
			t.Errorf("非法根模块路径 %q 应拒绝", path)
		}
	}
	for _, path := range []string{"go_wp", "example.org/team/cms/v2", "example.org/x"} {
		if _, err := ModuleFiles("member", path); err != nil {
			t.Errorf("合法根模块路径 %q 不该被拒绝: %v", path, err)
		}
	}
}

func TestModuleGoFilesParseAndFormat(t *testing.T) {
	files, err := ModuleFiles("member_card", "example.org/team/cms/v2")
	if err != nil {
		t.Fatalf("ModuleFiles: %v", err)
	}
	for rel, src := range files {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		if _, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.AllErrors); err != nil {
			t.Errorf("%s 无法解析: %v\n%s", rel, err, src)
			continue
		}
		formatted, err := format.Source([]byte(src))
		if err != nil {
			t.Errorf("%s gofmt 失败: %v", rel, err)
			continue
		}
		if string(formatted) != src {
			t.Errorf("%s 未按 gofmt 输出", rel)
		}
	}
}

func TestModuleFilesUseGivenRootAndSharedMessages(t *testing.T) {
	const root = "example.org/team/cms/v2"
	files, err := ModuleFiles("member_card", root)
	if err != nil {
		t.Fatalf("ModuleFiles: %v", err)
	}
	for rel, src := range files {
		if strings.Contains(src, "go_wp") {
			t.Errorf("%s 硬编码了 go_wp（应使用传入的根模块路径 %s）:\n%s", rel, root, src)
		}
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		// 不生成点分点消息键（模块消息复用 pkg/enums 与词库）。
		for _, dotted := range []string{".err.", ".msg."} {
			if strings.Contains(src, dotted) {
				t.Errorf("%s 生成了点分消息 %q", rel, dotted)
			}
		}
		// 错误出口不得把业务错误写成 200。
		if strings.Contains(src, "ErrorAuto(c, 200") || strings.Contains(src, "ErrorWithMessage(c, 200") {
			t.Errorf("%s 把错误出口写成 200", rel)
		}
	}

	// 导入必须指向传入的根模块路径（本模块包与框架 pkg 入口都是）。
	service := files["service/member_card_service.go"]
	for _, imp := range []string{
		root + "/internal/module/member_card/contract",
		root + "/internal/module/member_card/dto",
		root + "/internal/module/member_card/model",
		root + "/pkg/enums",
		root + "/pkg/facing",
	} {
		if !strings.Contains(service, imp) {
			t.Errorf("service 缺少导入 %s", imp)
		}
	}
	router := files["inbound/http/member_card_router.go"]
	for _, imp := range []string{root + "/pkg/middleware", root + "/internal/module/member_card/enums"} {
		if !strings.Contains(router, imp) {
			t.Errorf("router 缺少导入 %s", imp)
		}
	}
	// 路由必须登记真实权限对象，而不是空权限 / 豁免。
	if !strings.Contains(router, `g.GET("/demo", member_cardenums.DemoRead, handle.GetDemo)`) {
		t.Errorf("router 未把真实权限对象登记到 GET 路由:\n%s", router)
	}
	// DTO ID 为字符串并带 form/json + required/uuid 校验。
	req := files["dto/member_card_req.go"]
	if !strings.Contains(req, `ID string`) || !strings.Contains(req, `form:"id"`) ||
		!strings.Contains(req, `json:"id"`) || !strings.Contains(req, `binding:"required,uuid"`) {
		t.Errorf("请求 DTO 的 ID 绑定不完整:\n%s", req)
	}
	// 成功出口用公共 response.Success。
	if !strings.Contains(files["inbound/http/member_card_handle.go"], "response.Success(c, resp)") {
		t.Errorf("handler 未使用 response.Success")
	}
}

func TestWriteModuleRejectsExistingTargets(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.Mkdir("member", 0o755); err != nil {
		t.Fatalf("准备目录: %v", err)
	}
	if err := WriteModule("member", "go_wp"); err == nil {
		t.Fatal("目标目录已存在时应拒绝")
	}

	if err := os.WriteFile("plainfile", []byte("user content"), 0o644); err != nil {
		t.Fatalf("准备文件: %v", err)
	}
	if err := WriteModule("plainfile", "go_wp"); err == nil {
		t.Fatal("目标为普通文件时应拒绝")
	}
	if got, _ := os.ReadFile("plainfile"); string(got) != "user content" {
		t.Fatalf("拒绝时不得覆盖用户文件，实际 %q", got)
	}

	if err := os.Symlink(dir, "linked"); err != nil {
		t.Skipf("当前环境无法创建符号链接: %v", err)
	}
	if err := WriteModule("linked", "go_wp"); err == nil {
		t.Fatal("目标是符号链接时应拒绝")
	}
}

func TestWriteModuleWritesRealFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	const root = "example.org/team/cms/v2"
	if err := WriteModule("member", root); err != nil {
		t.Fatalf("WriteModule: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("member", "service", "member_service.go"))
	if err != nil {
		t.Fatalf("读取生成文件: %v", err)
	}
	if !strings.Contains(string(raw), root+"/internal/module/member/contract") {
		t.Fatalf("落盘文件未按根模块路径书写:\n%s", raw)
	}
	if _, err := os.Stat(filepath.Join("member", "README.md")); err != nil {
		t.Fatalf("缺 README: %v", err)
	}
}
