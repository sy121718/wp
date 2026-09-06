package pluginservice

// zip 安全解包测试（docs/06 §11）：zip slip 路径穿越、扩展名白名单、
// 大小限制、manifest 缺失。

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// buildZip 内存构造 zip（map[路径]内容）。
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip Create: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip Write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	return buf.Bytes()
}

// TestExtractZipSlip 路径穿越（../ 上溯）拒绝。
func TestExtractZipSlip(t *testing.T) {
	z := buildZip(t, map[string]string{
		"../evil.txt":      "x",
		"components/a.jet": "{{1}}",
		"sub/../../b.jet":  "{{2}}",
	})
	_, err := extractZipSafe(z)
	if err == nil || !strings.Contains(err.Error(), "穿越") {
		t.Fatalf("穿越路径应拒绝: %v", err)
	}
}

// TestExtractZipExtWhitelist 非白名单扩展名拒绝。
func TestExtractZipExtWhitelist(t *testing.T) {
	z := buildZip(t, map[string]string{
		"components/a.jet": "ok",
		"evil.sh":          "#!/bin/sh",
		"evil.php":         "<?php",
	})
	_, err := extractZipSafe(z)
	if err == nil || !strings.Contains(err.Error(), "白名单") {
		t.Fatalf("非白名单扩展名应拒绝: %v", err)
	}
}

// TestExtractZipLegal 合法包解包成功。
func TestExtractZipLegal(t *testing.T) {
	z := buildZip(t, map[string]string{
		"manifest.json":       `{"id":"mkt","name":"x","version":"1.0.0","components":[]}`,
		"components/card.jet": "<div></div>",
		"assets/card.css":     ".c{}",
	})
	files, err := extractZipSafe(z)
	if err != nil {
		t.Fatalf("合法包应解包成功: %v", err)
	}
	if _, ok := files["manifest.json"]; !ok {
		t.Fatalf("缺 manifest.json")
	}
	if _, ok := files["components/card.jet"]; !ok {
		t.Fatalf("缺组件模板")
	}
}
