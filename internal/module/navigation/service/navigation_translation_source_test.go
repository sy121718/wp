package navigationservice

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	"go_wp/pkg/i18n"
)

type sourceRowsStub struct {
	byProject map[string]map[string][]*navigationmodel.NavigationEntity
	failKinds map[string]error
	calls     []string
}

func (s *sourceRowsStub) ListForKind(_ context.Context, projectID, kind string) ([]*navigationmodel.NavigationEntity, error) {
	s.calls = append(s.calls, projectID+"|"+kind)
	if err, ok := s.failKinds[kind]; ok {
		return nil, err
	}
	return s.byProject[projectID][kind], nil
}

type sourceResolverStub struct {
	navigationcontract.SourceResolver
	titles map[string]string
	fail   map[string]error
	calls  []string
}

func (s *sourceResolverStub) ResolveSource(_ context.Context, _, _, sourceID string) (string, string, error) {
	s.calls = append(s.calls, sourceID)
	if err, ok := s.fail[sourceID]; ok {
		return "", "", err
	}
	if t, ok := s.titles[sourceID]; ok {
		return t, "/" + sourceID, nil
	}
	return "", "", nil
}

func navRow(id, title, parentID string) *navigationmodel.NavigationEntity {
	row := &navigationmodel.NavigationEntity{ID: id, Title: title, Path: "/p/" + id}
	if parentID != "" {
		p := parentID
		row.ParentID = &p
	}
	if strings.HasPrefix(id, "src:") {
		parts := strings.SplitN(strings.TrimPrefix(id, "src:"), ":", 2)
		row.SourceType = parts[0]
		sid := parts[1]
		row.SourceID = &sid
	}
	return row
}

func newSourceIndexService(rows sourceRows, resolver navigationcontract.SourceResolver) *Service {
	return &Service{m: nil, sources: resolver, sourceRowsPort: rows}
}

func sourceHash(text string) string { return i18n.ContentHash(text) }

func entryHashes(entries []navigationcontract.TranslationSourceEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Hash)
	}
	return out
}

func failedKinds(index navigationcontract.SourceIndex) []string {
	out := make([]string, 0)
	for _, f := range index.KindErrors() {
		out = append(out, f.Kind)
	}
	return out
}

func TestOpenSourceIndexCollectsAllFourKinds(t *testing.T) {
	shared := "关于我们"
	rows := &sourceRowsStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
		"p1": {
			"header":        {navRow("n1", shared, "")},
			"header_mobile": {navRow("n2", "移动首页", "")},
			"footer":        {navRow("n3", shared, ""), navRow("n4", "隐私政策", "n3")},
			"footer_mobile": {navRow("n5", "移动页脚", "")},
		},
	}}
	svc := newSourceIndexService(rows, nil)

	index, err := svc.OpenSourceIndex(context.Background(), "p1")
	if err != nil {
		t.Fatalf("OpenSourceIndex 失败: %v", err)
	}
	if got, want := index.Kinds(), []string{"header", "header_mobile", "footer", "footer_mobile"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Kinds() = %v，应为 %v（四 kind 全收集且按规范顺序）", got, want)
	}
	entry, ok := index.Locate(sourceHash(shared))
	if !ok {
		t.Fatalf("共享原文 %q 未收录", shared)
	}
	if got, want := entry.Kinds, []string{"header", "footer"}; !reflect.DeepEqual(got, want) {
		t.Errorf("共享原文的 Kinds = %v，应为 %v（同 source 跨 kind 共享）", got, want)
	}
	if entry.Context != i18n.ContentContext("navigation", "label") {
		t.Errorf("Context = %q，应与构建期语境逐字一致", entry.Context)
	}
	if entry.Source != shared {
		t.Errorf("Source = %q，应为服务端原文 %q", entry.Source, shared)
	}
	for _, want := range []string{"移动首页", "隐私政策", "移动页脚"} {
		if _, ok := index.Locate(sourceHash(want)); !ok {
			t.Errorf("四 kind 中任何位置的原文都应被收集，缺 %q", want)
		}
	}
	if len(index.KindErrors()) != 0 {
		t.Errorf("全部位置成功时 KindErrors 应为空，实际 %v", index.KindErrors())
	}
	if err := index.Require(); err != nil {
		t.Errorf("全部位置成功时 Require 应为 nil，实际 %v", err)
	}

	for _, call := range rows.calls {
		if !strings.HasPrefix(call, "p1|") {
			t.Errorf("行端口被以错误工程调用：%q", call)
		}
	}
}

func TestOpenSourceIndexStrictReadErrors(t *testing.T) {
	readErr := errors.New(`ERROR: relation "navigations" (SQLSTATE 42P01)`)
	rows := &sourceRowsStub{
		byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
			"p1": {"header": {navRow("n1", "首页", "")}},
		},
		failKinds: map[string]error{"footer_mobile": readErr},
	}
	svc := newSourceIndexService(rows, nil)

	index, err := svc.OpenSourceIndex(context.Background(), "p1")
	if err != nil {
		t.Fatalf("部分位置失败不应让整体打开失败: %v", err)
	}
	if got, want := failedKinds(index), []string{"footer_mobile"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("KindErrors = %v，应恰好一条 footer_mobile", got)
	}
	if got, want := index.Kinds(), []string{"header", "header_mobile", "footer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Kinds() = %v，应为读成的三个位置（失败位置不在其中）%v", got, want)
	}

	if _, ok := index.Locate(sourceHash("首页")); !ok {
		t.Error("失败位置之外的原文仍应可定位（读侧局部展示）")
	}

	requireErr := index.Require()
	if requireErr == nil {
		t.Fatal("有位置读失败时 Require 必须失败（写侧整体不写）")
	}
	if !errors.Is(requireErr, navigationenums.Err(navigationenums.ErrSourceKindFailed)) {
		t.Errorf("Require 错误应命中 ErrSourceKindFailed，实际 %v", requireErr)
	}
	if strings.Contains(requireErr.Error(), "SQLSTATE") || strings.Contains(requireErr.Error(), "navigations") {
		t.Errorf("对外错误不得含内部原文：%q", requireErr.Error())
	}
}

func TestOpenSourceIndexResolverFailureDropsWholeKind(t *testing.T) {
	resolverErr := errors.New("内容模块不可用 (SQLSTATE 08006)")
	resolver := &sourceResolverStub{
		titles: map[string]string{"pg1": "解析后的页面标题"},
		fail:   map[string]error{"ar1": resolverErr},
	}
	untouched := "页脚关于"
	rows := &sourceRowsStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
		"p1": {
			"header": {
				navRow("src:page:pg1", "旧记录标题", ""),
				navRow("src:article:ar1", "回退标题", ""),
				navRow("c1", "自定义项", ""),
			},
			"footer": {navRow("f1", untouched, "")},
		},
	}}
	svc := newSourceIndexService(rows, resolver)

	index, err := svc.OpenSourceIndex(context.Background(), "p1")
	if err != nil {
		t.Fatalf("单个位置失败不应让整体打开失败: %v", err)
	}

	for _, gone := range []string{"解析后的页面标题", "旧记录标题", "回退标题", "自定义项"} {
		if entry, ok := index.Locate(sourceHash(gone)); ok {
			t.Errorf("header 解析失败时该位置不应贡献条目，却定位到 %q：%+v", gone, entry)
		}
	}
	if entry, ok := index.Locate(sourceHash(untouched)); !ok || entry.Kinds[0] != "footer" {
		t.Errorf("其它位置仍应可读，footer 条目缺失或位置错：%+v / %v", entry, ok)
	}
	if got, want := failedKinds(index), []string{"header"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("KindErrors = %v，应恰好一条 header", got)
	}
	requireErr := index.Require()
	if !errors.Is(requireErr, navigationenums.Err(navigationenums.ErrSourceKindFailed)) {
		t.Fatalf("resolver 错误必须让写侧整体失败，实际 %v", requireErr)
	}
	if strings.Contains(requireErr.Error(), "SQLSTATE") || strings.Contains(requireErr.Error(), "内容模块") {
		t.Errorf("对外错误不得透出 resolver 原始错误：%q", requireErr.Error())
	}
}

func TestOpenSourceIndexWithoutSourceNodesNeedsNoResolution(t *testing.T) {
	t.Run("解析器已装但只有 custom 项", func(t *testing.T) {
		resolver := &sourceResolverStub{}
		rows := &sourceRowsStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
			"p1": {"header": {
				navRow("c1", "首页", ""),

				&navigationmodel.NavigationEntity{ID: "c2", Title: "博客", Path: "/blog", SourceType: "page"},
			}},
		}}
		svc := newSourceIndexService(rows, resolver)
		index, err := svc.OpenSourceIndex(context.Background(), "p1")
		if err != nil {
			t.Fatalf("OpenSourceIndex 失败: %v", err)
		}
		if len(resolver.calls) != 0 {
			t.Errorf("无来源实体节点不应触发 ResolveSource，实际调用 %v", resolver.calls)
		}
		if err := index.Require(); err != nil {
			t.Errorf("无来源节点不是失败事实，Require 应为 nil，实际 %v", err)
		}
		if _, ok := index.Locate(sourceHash("首页")); !ok {
			t.Error("custom 项标题仍应作为原文入索引（与 Tree 同源）")
		}
	})

	t.Run("未装解析器", func(t *testing.T) {
		rows := &sourceRowsStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
			"p1": {"header": {navRow("src:page:pg1", "记录标题", "")}},
		}}
		svc := newSourceIndexService(rows, nil)
		index, err := svc.OpenSourceIndex(context.Background(), "p1")
		if err != nil {
			t.Fatalf("解析器未装配不应让位置失败: %v", err)
		}
		if _, ok := index.Locate(sourceHash("记录标题")); !ok {
			t.Error("未装解析器时按记录自身标题入索引（与 Tree 一致）")
		}
		if err := index.Require(); err != nil {
			t.Errorf("未装解析器不是位置失败，Require 应为 nil，实际 %v", err)
		}
	})
}

func TestOpenSourceIndexAllKindsFailed(t *testing.T) {
	boom := errors.New("db down")
	rows := &sourceRowsStub{failKinds: map[string]error{
		"header": boom, "header_mobile": boom, "footer": boom, "footer_mobile": boom,
	}}
	svc := newSourceIndexService(rows, nil)
	index, err := svc.OpenSourceIndex(context.Background(), "p1")
	if err == nil || index != nil {
		t.Fatalf("四位置全失败必须显式失败，得到 index=%v err=%v", index, err)
	}
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrSourceKindFailed)) {
		t.Errorf("错误应命中 ErrSourceKindFailed，实际 %v", err)
	}
	if strings.Contains(err.Error(), "db down") {
		t.Errorf("对外错误不得含内部原文：%q", err.Error())
	}
	for _, want := range []string{"header", "header_mobile", "footer", "footer_mobile"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("聚合错误应列举全部失败位置，缺 %q：%v", want, err)
		}
	}
}

func TestOpenSourceIndexAllKindsResolverFailed(t *testing.T) {
	resolver := &sourceResolverStub{fail: map[string]error{
		"pg1": errors.New("page 不可用"), "ar1": errors.New("content 不可用"),
	}}
	rows := &sourceRowsStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
		"p1": {
			"header":        {navRow("src:page:pg1", "首页", "")},
			"header_mobile": {navRow("src:article:ar1", "文章", "")},
			"footer":        {navRow("src:page:pg1", "关于", "")},
			"footer_mobile": {navRow("src:article:ar1", "移动", "")},
		},
	}}
	svc := newSourceIndexService(rows, resolver)
	index, err := svc.OpenSourceIndex(context.Background(), "p1")
	if err == nil || index != nil {
		t.Fatalf("四位置均解析失败必须显式失败，得到 index=%v err=%v", index, err)
	}
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrSourceKindFailed)) {
		t.Errorf("错误应命中 ErrSourceKindFailed，实际 %v", err)
	}
	for _, want := range navigationSourceKinds {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("聚合错误应列举全部失败位置，缺 %q：%v", want, err)
		}
	}
}

func TestOpenSourceIndexRejectsEmptyProject(t *testing.T) {
	svc := newSourceIndexService(&sourceRowsStub{}, nil)
	if _, err := svc.OpenSourceIndex(context.Background(), "   "); !errors.Is(err, navigationenums.Err(navigationenums.ErrInvalidParam)) {
		t.Fatalf("空工程应为 ErrInvalidParam，实际 %v", err)
	}
}

func TestOpenSourceIndexPropagatesCancel(t *testing.T) {
	rows := &sourceRowsStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
		"p1": {"header": {navRow("n1", "首页", "")}},
	}}
	svc := newSourceIndexService(rows, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	index, err := svc.OpenSourceIndex(ctx, "p1")
	if index != nil {
		t.Fatalf("已取消的 ctx 不应返回索引，实际 %+v", index)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应原样传播 context.Canceled，实际 %v", err)
	}
	if errors.Is(err, navigationenums.Err(navigationenums.ErrSourceKindFailed)) {
		t.Errorf("取消不得伪装成来源读取失败：%v", err)
	}
}

func TestOpenSourceIndexMergesDuplicates(t *testing.T) {
	title := "产品中心"
	rows := &sourceRowsStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
		"p1": {
			"header": {navRow("n1", title, ""), navRow("n2", title, "n1"), navRow("n3", title, "")},
			"footer": {navRow("n4", title, "")},
		},
	}}
	svc := newSourceIndexService(rows, nil)
	index, err := svc.OpenSourceIndex(context.Background(), "p1")
	if err != nil {
		t.Fatalf("OpenSourceIndex 失败: %v", err)
	}
	if got := len(index.Entries()); got != 1 {
		t.Fatalf("同原文只应有一条 entry，实际 %d 条：%+v", got, index.Entries())
	}
	entry, _ := index.Locate(sourceHash(title))
	if got, want := entry.Kinds, []string{"header", "footer"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Kinds = %v，应为去重后的 %v", got, want)
	}
}

func TestOpenSourceIndexKeepsUntranslatableRows(t *testing.T) {
	rows := &sourceRowsStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
		"p1": {"header": {navRow("n1", "  ", ""), navRow("n2", "—", ""), navRow("n3", "2024", "")}},
	}}
	svc := newSourceIndexService(rows, nil)
	index, err := svc.OpenSourceIndex(context.Background(), "p1")
	if err != nil {
		t.Fatalf("OpenSourceIndex 失败: %v", err)
	}
	for _, title := range []string{"—", "2024"} {
		entry, ok := index.Locate(sourceHash(title))
		if !ok {
			t.Errorf("不可翻译的菜单文字 %q 仍应可定位（写侧要能给出准确原因）", title)
			continue
		}
		if i18n.ShouldTranslateContent(entry.Source) {
			t.Errorf("%q 不应被 ShouldTranslateContent 判为可翻译（判据留给写服务）", title)
		}
	}
}

func TestOpenSourceIndexIsDeterministic(t *testing.T) {
	titles := []string{"首页", "产品", "关于", "联系", "博客", "帮助", "服务", "案例", "团队", "招聘", "新闻", "条款"}
	headerRows := make([]*navigationmodel.NavigationEntity, 0, len(titles))
	for i, title := range titles {
		headerRows = append(headerRows, navRow("n"+string(rune('a'+i)), title, ""))
	}
	rows := &sourceRowsStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
		"p1": {"header": headerRows, "footer": {navRow("f1", "关于", "")}},
	}}
	svc := newSourceIndexService(rows, nil)
	first, err := svc.OpenSourceIndex(context.Background(), "p1")
	if err != nil {
		t.Fatalf("OpenSourceIndex 失败: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, aerr := svc.OpenSourceIndex(context.Background(), "p1")
		if aerr != nil {
			t.Fatalf("第 %d 次 OpenSourceIndex 失败: %v", i+2, aerr)
		}
		if !reflect.DeepEqual(first.Entries(), again.Entries()) {
			t.Fatalf("两次结果不一致：\n%v\n%v", first.Entries(), again.Entries())
		}
		if !reflect.DeepEqual(first.Kinds(), again.Kinds()) {
			t.Fatalf("两次 Kinds 不一致：%v vs %v", first.Kinds(), again.Kinds())
		}
	}
	hashes := entryHashes(first.Entries())
	for i := 1; i < len(hashes); i++ {
		if hashes[i-1] > hashes[i] {
			t.Fatalf("条目应按 hash 升序，实际 %v", hashes)
		}
	}
}

func TestOpenSourceIndexIsImmutable(t *testing.T) {
	rows := &sourceRowsStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{
		"p1": {"header": {navRow("n1", "首页", "")}, "footer": {navRow("n2", "首页", "")}},
	}}
	svc := newSourceIndexService(rows, nil)
	index, err := svc.OpenSourceIndex(context.Background(), "p1")
	if err != nil {
		t.Fatalf("OpenSourceIndex 失败: %v", err)
	}
	entry, _ := index.Locate(sourceHash("首页"))
	entry.Kinds[0] = "tampered"
	entry.Source = "tampered"
	kinds := index.Kinds()
	kinds[0] = "tampered"
	entries := index.Entries()
	entries[0].Source = "tampered"
	entries[0].Kinds[0] = "tampered"
	kindErrs := index.KindErrors()
	_ = kindErrs

	again, _ := index.Locate(sourceHash("首页"))
	if !reflect.DeepEqual(again.Kinds, []string{"header", "footer"}) || again.Source != "首页" {
		t.Fatalf("调用方修改返回值污染了索引内部：%+v", again)
	}
	if got := index.Kinds()[0]; got != "header" {
		t.Fatalf("Kinds() 返回切片被共享：%q", got)
	}
	if got := index.Entries()[0].Source; got != "首页" {
		t.Fatalf("Entries() 返回条目被共享：%q", got)
	}
	if got := index.Entries()[0].Kinds[0]; got != "header" {
		t.Fatalf("Entries() 返回条目的 Kinds 被共享：%q", got)
	}
}

func TestSourceIndexMergeHashConflict(t *testing.T) {
	hash := sourceHash("原始文案")
	index := &sourceIndex{byHash: map[string]int{}, kinds: []string{}, failures: []navigationcontract.KindReadError{}}
	ctx := context.Background()
	index.merge(ctx, navigationcontract.TranslationSourceEntry{Hash: hash, Source: "原始文案", Context: "navigation.label", Kinds: []string{kindHeader}}, kindHeader, "p1")
	index.merge(ctx, navigationcontract.TranslationSourceEntry{Hash: hash, Source: "另一段文案", Context: "navigation.label", Kinds: []string{kindFooter}}, kindFooter, "p1")
	index.kinds = []string{kindHeader, kindFooter}
	index.sortEntries()

	if got := len(index.entries); got != 1 {
		t.Fatalf("同 hash 只能保留一条 entry，实际 %d 条", got)
	}
	if got := index.entries[0].Source; got != "原始文案" {
		t.Errorf("冲突时应保留先出现的那条（不随机覆盖），实际 %q", got)
	}
	if got, want := failedKinds(index), []string{kindHeader, kindFooter}; !reflect.DeepEqual(got, want) {
		t.Fatalf("冲突涉及的两个位置都应记失败，实际 %v", got)
	}
	if !errors.Is(index.Require(), navigationenums.Err(navigationenums.ErrSourceKindFailed)) {
		t.Errorf("哈希冲突必须让写侧整体失败，实际 %v", index.Require())
	}
}

func TestSourceIndexMergeSameTextAcrossKinds(t *testing.T) {
	title := "服务"
	hash := sourceHash(title)
	index := &sourceIndex{byHash: map[string]int{}, kinds: []string{}, failures: []navigationcontract.KindReadError{}}
	ctx := context.Background()
	for _, kind := range []string{kindFooter, kindHeaderMobile} {
		index.merge(ctx, navigationcontract.TranslationSourceEntry{Hash: hash, Source: title, Context: "navigation.label", Kinds: []string{kind}}, kind, "p1")
	}
	if got, want := index.entries[0].Kinds, []string{kindHeaderMobile, kindFooter}; !reflect.DeepEqual(got, want) {
		t.Errorf("Kinds 应按规范顺序合并为 %v，实际 %v", want, got)
	}
	if len(index.failures) != 0 {
		t.Errorf("同原文跨 kind 不是冲突，不应记失败：%v", index.failures)
	}
}

func TestSourceIndexConflictIsDeterministic(t *testing.T) {
	hash := sourceHash("x")
	forward := &sourceIndex{byHash: map[string]int{}, kinds: []string{}, failures: []navigationcontract.KindReadError{}}
	backward := &sourceIndex{byHash: map[string]int{}, kinds: []string{}, failures: []navigationcontract.KindReadError{}}
	ctx := context.Background()
	a := navigationcontract.TranslationSourceEntry{Hash: hash, Source: "甲", Context: "navigation.label", Kinds: []string{kindHeader}}
	b := navigationcontract.TranslationSourceEntry{Hash: hash, Source: "乙", Context: "navigation.label", Kinds: []string{kindFooter}}
	forward.merge(ctx, a, kindHeader, "p1")
	forward.merge(ctx, b, kindFooter, "p1")
	backward.merge(ctx, b, kindFooter, "p1")
	backward.merge(ctx, a, kindHeader, "p1")

	if forward.entries[0].Source != "甲" || backward.entries[0].Source != "乙" {
		t.Fatalf("两边都应保留各自先出现的那条：forward=%q backward=%q", forward.entries[0].Source, backward.entries[0].Source)
	}
	if got, want := sortedCopy(failedKinds(forward)), sortedCopy(failedKinds(backward)); !reflect.DeepEqual(got, want) {
		t.Fatalf("两种顺序的失败位置集合应相同：%v vs %v", got, want)
	}
	if forward.Require() == nil || backward.Require() == nil {
		t.Error("两种顺序都必须让 Require 失败")
	}
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

func TestOpenSourceIndexWithoutModelFailsClosed(t *testing.T) {
	svc := &Service{}
	index, err := svc.OpenSourceIndex(context.Background(), "p1")
	if err == nil || index != nil {
		t.Fatalf("未装配时必须显式失败，得到 index=%v err=%v", index, err)
	}
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrProjectRequired)) {
		t.Errorf("应为 ErrProjectRequired，实际 %v", err)
	}
}

var _ sourceRows = (*sourceRowsStub)(nil)
var _ navigationcontract.SourceResolver = (*sourceResolverStub)(nil)
