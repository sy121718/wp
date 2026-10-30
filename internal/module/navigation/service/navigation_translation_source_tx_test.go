package navigationservice

// navigation_translation_source_tx_test.go — 事务内来源索引（OpenSourceIndexTx）与读侧**同一份事实**。
//
// 本文件钉的是三件此前会静默分叉的事：
//  1. 生产构造器真的注入了事务内行端口（rowsTx）—— 此前 NewService 只设 m/projects，
//     生产实例上 OpenSourceIndexTx 对每一次真实写译文都返回 ErrProjectRequired，
//     而全部单测都自己塞桩（桩绿、生产断）；
//  2. Tx 索引与读侧索引产出**逐字段相同**的条目（NodeID / Path / Kinds / Source / Hash），
//     条目拼装只有 kindEntriesOf 一份实现；
//  3. Tx 索引同样提供 SourceIndexNodes 的节点事实（读侧能定位节点、写侧不能曾是真实缺口）。
//
// 不碰 PG：行端口是桩，唯一被「真事务形状」约束的是 index 的构造参数 —— 这里用一个
// 真实 *sql.Tx 的空转句柄（newScopeOnlyTx，定义在 write_test 里，行为等价于「有句柄即可」）。

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

// txRowsStub 事务内行端口桩：与 sourceRowsStub 同形，记录的是 tx 版调用。
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

// newSourceIndexTxService 组装一个只装配了事务内行端口的 Service。
func newSourceIndexTxService(rows sourceRowsTxPort, resolver navigationcontract.SourceResolver) *Service {
	return &Service{m: nil, sources: resolver, rowsTx: rows}
}

// TestNewServiceInjectsRowsTx 生产构造器必须注入事务内行端口。
//
// 判据是「构造出来的 Service 能打开索引」，而不是「字段非 nil」：字段非 nil 只是手段，
// 能打开索引才是装配层与写路径真正依赖的能力。
func TestNewServiceInjectsRowsTx(t *testing.T) {
	svc := NewService(navigationmodel.NewModel(nil), nil)
	if svc.rowsTx == nil {
		t.Fatal("NewService 未注入 rowsTx：生产上 OpenSourceIndexTx 会恒返回 ErrProjectRequired")
	}
	// 没有事务句柄时显式失败（不返回一份「看起来是空的」索引）。
	if _, err := svc.OpenSourceIndexTx(context.Background(), nil, "3f0f1e3a-6d9a-4c0f-9a7e-6f6b1c2d3e4f"); !errors.Is(err, navigationenums.Err(navigationenums.ErrProjectRequired)) {
		t.Fatalf("缺事务句柄应返回 ErrProjectRequired，实际 %v", err)
	}
}

// TestSourceIndexTxMatchesReadSide 事务内索引与读侧索引逐字段相同。
//
// 同一份行、同一个 resolver，两条入口产出的条目必须**完全相等**（Hash / Source /
// Context / Kinds / NodeID / Path），否则「读侧展示的原文」与「写侧校验的原文」是两份事实。
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
	// NodeID / Path 是读侧展示的事实：写成空串等于读侧定位不到节点。
	for _, e := range txIndex.Entries() {
		if e.NodeID == "" {
			t.Errorf("条目 %q 缺 NodeID", e.Source)
		}
		if !strings.HasPrefix(e.Path, "/") {
			t.Errorf("条目 %q 的 Path = %q，应为解析后的链接", e.Source, e.Path)
		}
	}
}

// TestSourceIndexTxProvidesNodes 事务内索引也提供节点事实（SourceIndexNodes）。
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

// TestSourceIndexTxRejectsMissingPort 未装配行端口时显式失败（绝不返回「看起来是空的」索引）。
//
// 分档要点：缺端口是**装配缺陷**，必须与「四个位置都读失败」区分开。若不预先判，
// 同一个装配错误会被复述成 ErrSourceKindFailed: header, header_mobile, footer, footer_mobile
// —— 那是一条业务结论（每个位置都读不到），会让排查方向从装配转到数据/数据库。
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

// TestSourceIndexTxIsTheProductionPath 生产构造出来的 Service 必须能走通事务内索引。
//
// 这条用例锁的是「NewService 注入的 rowsTx 就是 OpenSourceIndexTx 实际使用的那一个」：
// 只断言字段非 nil 挡不住「注入了但用不到」；这里用一个带可识别行的桩替换该字段，
// 再断言索引里真的出现了这行——说明写路径读的就是这个端口。
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

// TestSourceIndexTxRequireAggregatesFailures 任一位置读失败 → Require 整体失败（写侧严格）。
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
