package navigationservice

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	"go_wp/pkg/i18n"
)

type txRowsStub struct {
	byProject map[string]map[string][]*navigationmodel.NavigationEntity
	failKinds map[string]error
	calls     []string
}

func (s *txRowsStub) ListForKindTx(_ context.Context, _ *gorm.DB, projectID, kind string) ([]*navigationmodel.NavigationEntity, error) {
	s.calls = append(s.calls, projectID+"|"+kind)
	if err, ok := s.failKinds[kind]; ok {
		return nil, err
	}
	return s.byProject[projectID][kind], nil
}

func newSourceIndexTxService(rows sourceRowsTxPort, resolver navigationcontract.SourceResolver) *Service {
	return &Service{m: nil, sources: resolver, rowsTx: rows}
}

func TestNewServiceInjectsRowsTx(t *testing.T) {
	svc := NewService(navigationmodel.NewModel(nil), nil)
	if svc.rowsTx == nil {
		t.Fatal("NewService 未注入 rowsTx：生产上 OpenSourceIndexTx 会恒返回 ErrProjectRequired")
	}

	if _, err := svc.OpenSourceIndexTx(context.Background(), nil, "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f"); !errors.Is(err, navigationenums.Err(navigationenums.ErrProjectRequired)) {
		t.Fatalf("缺事务句柄应返回 ErrProjectRequired，实际 %v", err)
	}
}

func TestSourceIndexTxMatchesReadSide(t *testing.T) {
	shared := "关于我们"
	rows := map[string]map[string][]*navigationmodel.NavigationEntity{
		"p1": {
			"header":        {navRow("src:page:pg1", shared, "")},
			"header_mobile": {navRow("n2", "移动首页", "")},
			"footer":        {navRow("n3", shared, ""), navRow("n4", "隐私政策", "n3")},
			"footer_mobile": {navRow("n5", "移动页脚", "")},
		},
	}
	resolver := &sourceResolverStub{titles: map[string]string{"pg1": shared}}
	readSvc := newSourceIndexService(&sourceRowsStub{byProject: rows}, resolver)
	txSvc := newSourceIndexTxService(&txRowsStub{byProject: rows}, resolver)

	readIndex, err := readSvc.OpenSourceIndex(context.Background(), "p1")
	if err != nil {
		t.Fatalf("读侧 OpenSourceIndex 失败: %v", err)
	}
	txIndex, err := txSvc.OpenSourceIndexTx(context.Background(), newScopeOnlyTx(t), "p1")
	if err != nil {
		t.Fatalf("写侧 OpenSourceIndexTx 失败: %v", err)
	}
	if got, want := txIndex.Entries(), readIndex.Entries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("事务内索引与读侧索引的条目必须逐字段相同：\n got=%+v\nwant=%+v", got, want)
	}

	for _, e := range txIndex.Entries() {
		if e.NodeID == "" {
			t.Errorf("条目 %q 缺 NodeID", e.Source)
		}
		if !strings.HasPrefix(e.Path, "/") {
			t.Errorf("条目 %q 的 Path = %q，应为解析后的链接", e.Source, e.Path)
		}
	}
}

func TestSourceIndexTxProvidesNodes(t *testing.T) {
	shared := "关于我们"
	rows := map[string]map[string][]*navigationmodel.NavigationEntity{
		"p1": {
			"header": {navRow("n1", shared, "")},
			"footer": {navRow("n2", shared, "")},
		},
	}
	svc := newSourceIndexTxService(&txRowsStub{byProject: rows}, nil)
	index, err := svc.OpenSourceIndexTx(context.Background(), newScopeOnlyTx(t), "p1")
	if err != nil {
		t.Fatalf("OpenSourceIndexTx 失败: %v", err)
	}
	nodeIndex, ok := index.(navigationcontract.SourceIndexNodes)
	if !ok {
		t.Fatal("事务内索引必须实现 SourceIndexNodes：写侧与读侧是同一个索引类型")
	}
	nodes := nodeIndex.Nodes(i18n.ContentHash(shared))
	if len(nodes) != 2 {
		t.Fatalf("同一段原文出现在两个位置，应有两个节点，实际 %+v", nodes)
	}
	if nodes[0].Kind != "header" || nodes[1].Kind != "footer" {
		t.Fatalf("节点顺序应按规范 kind 顺序，实际 %+v", nodes)
	}
	if nodes[0].ID != "n1" || nodes[0].Path != "/p/n1" {
		t.Fatalf("节点事实（id / path）应来自该位置的菜单行，实际 %+v", nodes[0])
	}
}

func TestSourceIndexTxRejectsMissingPort(t *testing.T) {
	svc := &Service{}
	_, err := svc.OpenSourceIndexTx(context.Background(), newScopeOnlyTx(t), "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f")
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrProjectRequired)) {
		t.Fatalf("缺行端口应返回 ErrProjectRequired（装配缺陷），实际 %v", err)
	}
	if errors.Is(err, navigationenums.Err(navigationenums.ErrSourceKindFailed)) {
		t.Fatalf("缺行端口不得被复述成「四个位置都读失败」: %v", err)
	}
}

func TestSourceIndexTxIsTheProductionPath(t *testing.T) {
	svc := NewService(navigationmodel.NewModel(nil), nil)
	rows := &txRowsStub{
		byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
			"p1": {"header": {navRow("n1", "页眉", "")}},
		},
	}
	svc.rowsTx = rows
	index, err := svc.OpenSourceIndexTx(context.Background(), newScopeOnlyTx(t), "p1")
	if err != nil {
		t.Fatalf("生产构造的 Service 应能打开事务内索引: %v", err)
	}
	if got := len(index.Entries()); got != 1 {
		t.Fatalf("事务内索引应读到桩里的那 1 行，实际 %d 条", got)
	}
	if reqErr := index.Require(); reqErr != nil {
		t.Fatalf("完整索引不应报位置失败: %v", reqErr)
	}
}

func TestSourceIndexTxRequireAggregatesFailures(t *testing.T) {
	rows := &txRowsStub{
		byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
			"p1": {"header": {navRow("n1", "页眉", "")}},
		},
		failKinds: map[string]error{"footer_mobile": errors.New("端口不可用")},
	}
	svc := newSourceIndexTxService(rows, nil)
	index, err := svc.OpenSourceIndexTx(context.Background(), newScopeOnlyTx(t), "p1")
	if err != nil {
		t.Fatalf("部分失败不该让整份索引打不开（由 Require 决定写侧口径）: %v", err)
	}
	if failed := failedKinds(index); !reflect.DeepEqual(failed, []string{"footer_mobile"}) {
		t.Fatalf("KindErrors 应只含失败位置，实际 %v", failed)
	}
	if rerr := index.Require(); !errors.Is(rerr, navigationenums.Err(navigationenums.ErrSourceKindFailed)) {
		t.Fatalf("Require 应在任一位置失败时整体失败，实际 %v", rerr)
	}
}
