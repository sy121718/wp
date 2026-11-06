package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunModuleRequiresTwoArguments(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	for _, args := range [][]string{
		{"module"},
		{"module", "member"},
		{"module", "member", "example.org/team/cms/v2", "extra"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 1 {
			t.Fatalf("run(%v) = %d，want 1", args, code)
		}
		if !strings.Contains(stderr.String(), "plugin module <name> <module-path>") {
			t.Fatalf("run(%v) 未给出正确用法: %q", args, stderr.String())
		}
		if _, err := os.Stat("member"); !os.IsNotExist(err) {
			t.Fatalf("参数不全时不应生成目录")
		}
	}
}

func TestRunModulePassesModulePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	const root = "example.org/team/cms/v2"
	var stdout, stderr bytes.Buffer
	if code := run([]string{"module", "member", root}, &stdout, &stderr); code != 0 {
		t.Fatalf("run = %d，stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), root) {
		t.Fatalf("成功输出应回显根模块路径: %q", stdout.String())
	}
	raw, err := os.ReadFile(filepath.Join("member", "dto", "member_resp.go"))
	if err != nil {
		t.Fatalf("读取生成文件: %v", err)
	}
	if !strings.Contains(string(raw), "package memberdto") {
		t.Fatalf("落盘内容不符合生成器输出:\n%s", raw)
	}
	svc, err := os.ReadFile(filepath.Join("member", "service", "member_service.go"))
	if err != nil {
		t.Fatalf("读取 service: %v", err)
	}
	if !strings.Contains(string(svc), root+"/internal/module/member/contract") {
		t.Fatalf("生成导入未使用传入的根模块路径:\n%s", svc)
	}
}
