package unit

import (
	"context"
	publish "go_wp/pkg/publish"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	pubcontract "go_wp/internal/module/publication/contract"
)

func enableLangs(t *testing.T, f *presFixture, langs ...string) {
	t.Helper()
	for i, lang := range langs {
		if err := f.db.Exec(
			"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) VALUES (?, ?, ?, ?, true, now(), now())",
			f.projectID, lang, i, i == 0).Error; err != nil {
			t.Fatalf("写入语言 %s 清单失败: %v", lang, err)
		}
	}
}

type pubLangRow struct {
	Lang         string
	ActivePath   string
	ArtifactHash string
}

func publicationRows(t *testing.T, f *presFixture, instanceID string) []pubLangRow {
	t.Helper()
	var rows []pubLangRow
	if err := f.db.Raw(
		"SELECT lang, active_path, artifact_hash FROM presentation_publications WHERE presentation_id = ? ORDER BY lang",
		instanceID).Scan(&rows).Error; err != nil {
		t.Fatalf("查询语言账本失败: %v", err)
	}
	return rows
}

func samePublicationRows(a, b []pubLangRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func instancePointer(t *testing.T, f *presFixture, instanceID string) (activeArtifact *string, stale bool) {
	t.Helper()
	var row struct {
		Active *string
		Stale  bool
	}
	if err := f.db.Raw(
		"SELECT active_artifact_id AS active, stale FROM presentation_instances WHERE id = ?",
		instanceID).Scan(&row).Error; err != nil {
		t.Fatalf("查询实例指针失败: %v", err)
	}
	return row.Active, row.Stale
}

func activeHash(t *testing.T, path string) string {
	t.Helper()
	store := &publish.LocalPublicationStore{ActiveRoot: publish.ActiveRoot()}
	state, err := store.Inspect(path)
	if err != nil || state == nil || state.Locator == nil {
		return ""
	}
	return strings.TrimPrefix(state.Locator.Key, "artifacts/")
}

func assertLedgerMatchesAccessSurface(t *testing.T, f *presFixture, instanceID string) {
	t.Helper()
	rows := publicationRows(t, f, instanceID)
	if len(rows) == 0 {
		t.Fatal("语言账本不应为空")
	}
	for _, row := range rows {
		if got := activeHash(t, row.ActivePath); got != row.ArtifactHash {
			t.Fatalf("语言 %s 的账本声称 %s 已发布（hash %s），访问面实际 %q",
				row.Lang, row.ActivePath, row.ArtifactHash, got)
		}
	}
}

func TestPresentationMultiLangPartialFailureNeverClaimsUnpublished(t *testing.T) {
	injector := &routeFaultInjector{}
	f := newPresFixtureWithRoutes(t, func(routes pubcontract.PublicationService) pubcontract.PublicationService {
		injector.PublicationService = routes
		return injector
	})
	if f == nil {
		return
	}
	ctx := context.Background()
	enableLangs(t, f, "zh-CN", "en-US", "ja-JP")
	f.createTemplate(t)

	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "i18n-partial-shirt",
		Data: map[string]any{"title": "多语言批次衬衫", "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	logicalPath := "/products/i18n-partial-shirt"
	enPath := "/en/products/i18n-partial-shirt"

	injector.setFailPath(enPath)
	_, err = f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: logicalPath,
	})
	injector.setFailPath("")
	if err == nil {
		t.Fatal("某一语言发布失败时整体发布必须返回错误")
	}

	var instID string
	if err = f.db.Raw("SELECT id FROM presentation_instances WHERE project_id = ? AND entity_id = ?",
		f.projectID, entity.ID).Scan(&instID).Error; err != nil || instID == "" {
		t.Fatalf("实例应已建行: instID=%q err=%v", instID, err)
	}

	rows := publicationRows(t, f, instID)
	if len(rows) != 0 {
		t.Fatalf("整批回滚后不应有任何语言账本行，实际 %+v", rows)
	}

	if active, stale := instancePointer(t, f, instID); active != nil {
		t.Fatalf("整批未成功时实例活跃指针不应前进，实际 %v", *active)
	} else if !stale {
		t.Fatal("批次未收敛时实例应标记 stale（给后台一个明确的 partial 信号）")
	}

	recovered, _, rerr := f.pres.RecoverPendingPublications(ctx)
	if rerr != nil {
		t.Fatalf("启动恢复失败: %v", rerr)
	}

	_ = recovered
	if pending, _ := receiptStates(t, f, instID); pending != 0 {
		t.Fatalf("恢复后不应再有 pending 回执，实际 %d", pending)
	}

	rows = publicationRows(t, f, instID)
	if len(rows) != 3 {
		t.Fatalf("恢复后三种语言都应结案，实际 %+v", rows)
	}
	assertLedgerMatchesAccessSurface(t, f, instID)
	active, stale := instancePointer(t, f, instID)
	if active == nil {
		t.Fatal("批次收敛后实例活跃指针应指向默认语言产物")
	}
	if stale {
		t.Fatal("批次收敛后实例不应再标记 stale")
	}

	var primaryArtifact string
	for _, row := range rows {
		if row.Lang == "zh-CN" {
			primaryArtifact = row.ArtifactHash
		}
	}
	if got := activeHash(t, logicalPath); got == "" || got != primaryArtifact {
		t.Fatalf("默认语言账本 hash %s 与访问面 %q 不一致", primaryArtifact, got)
	}

	recovered, rolledBack, rerr := f.pres.RecoverPendingPublications(ctx)
	if rerr != nil || recovered != 0 || rolledBack != 0 {
		t.Fatalf("重复恢复应为空操作，实际 recovered=%d rolledBack=%d err=%v", recovered, rolledBack, rerr)
	}
	if rows = publicationRows(t, f, instID); len(rows) != 3 {
		t.Fatalf("重复恢复后语言账本行数应保持 3，实际 %+v", rows)
	}
}

type artifactBatchRow struct {
	Lang       string
	Version    int64
	SnapshotID string
}

func ledgerBatch(t *testing.T, f *presFixture, instanceID string) []artifactBatchRow {
	t.Helper()
	var rows []artifactBatchRow
	if err := f.db.Raw(
		"SELECT pp.lang, pa.version, pa.snapshot_id FROM presentation_publications pp "+
			"JOIN presentation_artifacts pa ON pa.id = pp.artifact_id WHERE pp.presentation_id = ? ORDER BY pp.lang",
		instanceID).Scan(&rows).Error; err != nil {
		t.Fatalf("查询账本产物批次失败: %v", err)
	}
	return rows
}

func hreflangPairs(html string) [][2]string {
	out := [][2]string{}
	for _, line := range strings.Split(html, "\n") {
		if !strings.Contains(line, "rel=\"alternate\"") || !strings.Contains(line, "hreflang=") {
			continue
		}
		lang := between(line, "hreflang=\"", "\"")
		href := between(line, "href=\"", "\"")
		if lang == "" || href == "" {
			continue
		}
		out = append(out, [2]string{lang, href})
	}
	return out
}

func between(s, open, close string) string {
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func artifactHTML(t *testing.T, hash string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(publish.DefaultArtifactRoot(), "artifacts", hash, "index.html"))
	if err != nil {
		t.Fatalf("读取产物 %s 失败: %v", hash, err)
	}
	return string(b)
}

func assertHreflangComplete(t *testing.T, label, html string, want [][2]string) {
	t.Helper()
	got := hreflangPairs(html)
	t.Logf("%s 的 hreflang 互指：%v", label, got)
	if len(got) != len(want) {
		t.Fatalf("%s 的 hreflang 互指应有 %d 条，实际 %d 条：%+v", label, len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s 的 hreflang 第 %d 条应为 %v，实际 %v（全部：%+v）", label, i+1, want[i], got[i], got)
		}
	}
}

func TestPresentationMultiLangRebuildIdempotent(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	enableLangs(t, f, "zh-CN", "en-US", "ja-JP")
	f.createTemplate(t)

	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "i18n-idempotent-shirt",
		Data: map[string]any{"title": "幂等衬衫", "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
		URLPath: "/products/i18n-idempotent-shirt",
	})
	if err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}
	first := publicationRows(t, f, inst.ID)
	if len(first) != 3 {
		t.Fatalf("首发布应有三种语言结案，实际 %+v", first)
	}
	assertLedgerMatchesAccessSurface(t, f, inst.ID)

	want := [][2]string{
		{"en-US", "/en/products/i18n-idempotent-shirt"},
		{"ja-JP", "/ja/products/i18n-idempotent-shirt"},
		{"zh-CN", "/products/i18n-idempotent-shirt"},
		{"x-default", "/products/i18n-idempotent-shirt"},
	}
	for _, row := range first {
		assertHreflangComplete(t, "首发布 "+row.Lang, artifactHTML(t, row.ArtifactHash), want)
	}

	if rerr := f.pres.RebuildInstance(ctx, inst.ID); rerr != nil {
		t.Fatalf("重建失败: %v", rerr)
	}
	settled := publicationRows(t, f, inst.ID)
	if !samePublicationRows(first, settled) {
		t.Fatalf("首发布产物 hash 必须与紧随其后的重建一致（SEO-026），实际\n首发布：%+v\n重建后：%+v", first, settled)
	}
	settledBatch := ledgerBatch(t, f, inst.ID)
	if len(settledBatch) != 3 {
		t.Fatalf("收敛后应有三条批次行，实际 %+v", settledBatch)
	}

	for i := 0; i < 2; i++ {
		if rerr := f.pres.RebuildInstance(ctx, inst.ID); rerr != nil {
			t.Fatalf("第 %d 次重复重建失败: %v", i+1, rerr)
		}
		rows := publicationRows(t, f, inst.ID)
		if !samePublicationRows(settled, rows) {
			t.Fatalf("重复重建不应改变语言账本：%+v → %+v", settled, rows)
		}
		batch := ledgerBatch(t, f, inst.ID)
		for _, row := range batch {
			for _, base := range settledBatch {
				if row.Lang == base.Lang && (row.Version != base.Version || row.SnapshotID != base.SnapshotID) {
					t.Fatalf("重复重建产生了新批次：%+v → %+v", settledBatch, batch)
				}
			}
		}
	}
	assertLedgerMatchesAccessSurface(t, f, inst.ID)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = f.pres.RebuildInstance(ctx, inst.ID)
		}(i)
	}
	wg.Wait()
	for i, rerr := range errs {
		if rerr != nil {
			t.Fatalf("并发重建第 %d 个失败: %v", i+1, rerr)
		}
	}

	batch := ledgerBatch(t, f, inst.ID)
	if len(batch) != 3 {
		t.Fatalf("并发重建后应有三条账本批次行，实际 %+v", batch)
	}
	for _, row := range batch[1:] {
		if row.Version != batch[0].Version || row.SnapshotID != batch[0].SnapshotID {
			t.Fatalf("并发重建产生了混合版本：%+v", batch)
		}
	}
	assertLedgerMatchesAccessSurface(t, f, inst.ID)
	if active, stale := instancePointer(t, f, inst.ID); active == nil || stale {
		t.Fatalf("并发重建后实例应已发布且非 stale，实际 active=%v stale=%v", active, stale)
	}
}
