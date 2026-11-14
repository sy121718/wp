package feature

import (
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	"go_wp/pkg/builder"
)

// 克隆从真实持久化文档读取，只返回重建节点 ID 的 AST，不改源块或版本。
func TestBlockCloneReadsPersistedDocumentWithoutWriting(t *testing.T) {
	env := newRefEnv(t)
	if env == nil {
		return
	}
	id := env.createBlock("编辑器克隆源")
	source, err := env.blocks.Detail(t.Context(), &blockcontract.DetailReq{ProjectID: env.projectID, ID: id})
	if err != nil {
		t.Fatal(err)
	}
	// HTTP 原生 JSON 与 query 隔离另由模块绑定测试覆盖，这里只核真实库读取及无写副作用。
	clonedResponse, err := env.blocks.CloneAST(t.Context(), &blockdto.CloneReq{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	original, err := builder.ParsePage(source.Document)
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := builder.ParsePage(clonedResponse.Document)
	if err != nil {
		t.Fatal(err)
	}
	if len(cloned.Root) != len(original.Root) || len(cloned.Root) != 1 || cloned.Root[0].ID == original.Root[0].ID || cloned.Root[0].ID == "" {
		t.Fatalf("克隆必须保持节点数并分配新 ID：original=%s cloned=%s", source.Document, clonedResponse.Document)
	}
	if !strings.Contains(string(clonedResponse.Document), "占位") {
		t.Fatalf("克隆必须保留已存内容：%s", clonedResponse.Document)
	}
	after, err := env.blocks.Detail(t.Context(), &blockcontract.DetailReq{ProjectID: env.projectID, ID: id})
	if err != nil || after.Version != source.Version || string(after.Document) != string(source.Document) {
		t.Fatalf("读取克隆不得改源文档或版本：after=%+v err=%v", after, err)
	}
}
