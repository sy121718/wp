package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWritePreviewArtifact(t *testing.T) {
	t.Parallel()

	pageID := "123e4567-e89b-42d3-a456-426614174000"
	document := json.RawMessage(`{"schemaVersion":2,"id":"` + pageID + `"}`)
	request := generateRequest{
		PageID:   pageID,
		HTML:     "<!DOCTYPE html><html><body>preview</body></html>",
		Document: document,
	}
	root := t.TempDir()
	if err := writePreviewArtifact(root, request); err != nil {
		t.Fatalf("写入测试产物失败: %v", err)
	}

	html, err := os.ReadFile(filepath.Join(root, pageID, "index.html"))
	if err != nil {
		t.Fatalf("读取 HTML 产物失败: %v", err)
	}
	if string(html) != request.HTML {
		t.Fatalf("HTML 产物内容不一致: %q", html)
	}

	source, err := os.ReadFile(filepath.Join(root, pageID, "document.json"))
	if err != nil {
		t.Fatalf("读取 Page Document 失败: %v", err)
	}
	if string(source) != string(document) {
		t.Fatalf("Page Document 内容不一致: %q", source)
	}
}

func TestValidateGenerateRequestRejectsInvalidIdentity(t *testing.T) {
	t.Parallel()

	tests := []generateRequest{
		{
			PageID:   "../../outside",
			HTML:     "<!DOCTYPE html><html></html>",
			Document: json.RawMessage(`{"schemaVersion":2,"id":"../../outside"}`),
		},
		{
			PageID:   "123e4567-e89b-42d3-a456-426614174000",
			HTML:     "<html></html>",
			Document: json.RawMessage(`{"schemaVersion":2,"id":"123e4567-e89b-42d3-a456-426614174000"}`),
		},
		{
			PageID:   "123e4567-e89b-42d3-a456-426614174000",
			HTML:     "<!DOCTYPE html><html></html>",
			Document: json.RawMessage(`{"schemaVersion":1,"id":"123e4567-e89b-42d3-a456-426614174000"}`),
		},
	}

	for index, request := range tests {
		if err := validateGenerateRequest(request); err == nil {
			t.Fatalf("非法请求 %d 未被拒绝", index)
		}
	}
}
