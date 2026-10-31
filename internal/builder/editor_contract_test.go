package builder

import (
	"bytes"
	"os"
	"testing"
)

func TestEditorContractsGeneratedFileIsCurrent(t *testing.T) {
	want, err := EditorContractsJS()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../templates/static/js/workbench/generated-contracts.js")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("工作台契约漂移：在仓库根目录执行 go run ./cmd/workbench-contracts")
	}
}
