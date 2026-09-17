// presentation_i18n_partial_test.go — 多语言发布的「数据库不能声称未上线的语言已发布」（审计 AR2-003）。
//
// 缺陷原状：publishAllLangs 先在事务里提交全部语言的 publication 行与实例活跃指针，
// 事务提交之后才逐个激活文件；第二种语言激活失败时，数据库显示三种语言都已发布，
// 而线上只有第一种语言的文件 —— 后台、sitemap、语言切换器都会报出不可访问的 URL。
//
// 本用例断言的三条不变式：
//
//	I1 语言账本行（presentation_publications）存在的语言，其 active_path 在访问面上
//	   确实指向该行记录的产物（DB 声称已发布 ⇒ 文件真的在线）；
//	I2 实例活跃指针只在整批成功后才前进（指针落后于访问面是安全方向，反之不是）；
//	I3 失败后实例 stale=true 给出「明确 partial」信号，启动恢复能把批次收敛回全成功。
package unit

import (
	"context"
	"strings"
	"sync"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
)

// enableLangs 写入语言清单（首个语言为默认语言），sort_order 按传入顺序。
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

// pubLangRow 某语言的发布账本行。
type pubLangRow struct {
	Lang         string
	ActivePath   string
	ArtifactHash string
}

// publicationRows 读取实例的语言账本行。
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

// samePublicationRows 两组账本行是否逐字相同。
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

// instancePointer 读取实例活跃指针与 stale 标记。
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

// activeHash 访问面上该路径当前指向的产物哈希（未激活为空串）。
func activeHash(t *testing.T, path string) string {
	t.Helper()
	store := &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}
	state, err := store.Inspect(path)
	if err != nil || state == nil || state.Locator == nil {
		return ""
	}
	return strings.TrimPrefix(state.Locator.Key, "artifacts/")
}

// assertLedgerMatchesAccessSurface I1：账本里每个语言都真的在访问面上指向记录的产物。
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

// TestPresentationMultiLangPartialFailureNeverClaimsUnpublished 第二种语言发布失败后，
// 数据库不得声称它（以及它之后的语言）已发布；启动恢复后批次收敛为全成功。
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
	i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain)
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })
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

	// 故障注入：第二种语言（en-US）的路由登记失败 —— 与「该语言激活失败」同一后果：
	// 它没有结案，它之后的语言也不会开始。
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

	// 失败后：只有走完结案的语言有账本行；失败语言与它之后的语言一律没有。
	rows := publicationRows(t, f, instID)
	if len(rows) != 1 || rows[0].Lang != "zh-CN" {
		t.Fatalf("失败后只应有 zh-CN 一行语言账本，实际 %+v", rows)
	}
	// I1：已有的那一行必须与访问面一致（不许「账本说有、文件没有」）。
	assertLedgerMatchesAccessSurface(t, f, instID)
	// I2：整批未成功，活跃指针不得前进。
	if active, stale := instancePointer(t, f, instID); active != nil {
		t.Fatalf("整批未成功时实例活跃指针不应前进，实际 %v", *active)
	} else if !stale {
		t.Fatal("批次未收敛时实例应标记 stale（给后台一个明确的 partial 信号）")
	}

	// 重启恢复：故障消除后，回执补齐已生效语言，未铺满的批次自动重跑一次。
	recovered, _, rerr := f.pres.RecoverPendingPublications(ctx)
	if rerr != nil {
		t.Fatalf("启动恢复失败: %v", rerr)
	}
	if recovered < 1 {
		t.Fatalf("en-US 的切换已生效，应至少补齐 1 条回执，实际 %d", recovered)
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
	// 收敛后默认语言的账本行必须就是指针指向的产物（两者不能各说各话）。
	var primaryArtifact string
	for _, row := range rows {
		if row.Lang == "zh-CN" {
			primaryArtifact = row.ArtifactHash
		}
	}
	if got := activeHash(t, logicalPath); got == "" || got != primaryArtifact {
		t.Fatalf("默认语言账本 hash %s 与访问面 %q 不一致", primaryArtifact, got)
	}

	// 重复恢复是空操作（幂等）。
	recovered, rolledBack, rerr := f.pres.RecoverPendingPublications(ctx)
	if rerr != nil || recovered != 0 || rolledBack != 0 {
		t.Fatalf("重复恢复应为空操作，实际 recovered=%d rolledBack=%d err=%v", recovered, rolledBack, rerr)
	}
	if rows = publicationRows(t, f, instID); len(rows) != 3 {
		t.Fatalf("重复恢复后语言账本行数应保持 3，实际 %+v", rows)
	}
}

// artifactBatchRow 某语言账本行指向的产物批次标识（version + 快照）。
type artifactBatchRow struct {
	Lang       string
	Version    int64
	SnapshotID string
}

// ledgerBatch 各语言账本行指向的产物属于哪一批（version / snapshot 应完全一致）。
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

// TestPresentationMultiLangRebuildIdempotent 重复重建与并发重建都不产生混合版本。
//
// 幂等口径取「收敛之后的连续重建」：首次发布的产物与重建后的产物在 hreflang 上有
// 一处**既有**差异（LocaleView 的 published 回调按语言账本过滤互指，首发布时同批的
// 其他语言尚未结案，于是没有互指链接），这不是本次改动引入的，也不在 AR2-003 范围。
func TestPresentationMultiLangRebuildIdempotent(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain)
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })
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
	if rows := publicationRows(t, f, inst.ID); len(rows) != 3 {
		t.Fatalf("首发布应有三种语言结案，实际 %+v", rows)
	}
	assertLedgerMatchesAccessSurface(t, f, inst.ID)

	// 先做一次重建让产物收敛（见函数注释里的 hreflang 既有差异），以此为幂等基线。
	if rerr := f.pres.RebuildInstance(ctx, inst.ID); rerr != nil {
		t.Fatalf("收敛重建失败: %v", rerr)
	}
	settled := publicationRows(t, f, inst.ID)
	settledBatch := ledgerBatch(t, f, inst.ID)
	if len(settledBatch) != 3 {
		t.Fatalf("收敛后应有三条批次行，实际 %+v", settledBatch)
	}

	// 重复重建：同内容复用产物行，账本行与批次都不变。
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

	// 并发重建：实例锁把两次发布串行化，结束后不得出现「一部分语言指向旧批次、
	// 另一部分指向新批次」的混合版本。
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
