package unit

// page_url_lang_freeze_test.go — 改 URL 的语言集合必须在切访问面之前冻结（审计 I18N-02 收尾）。
//
// 报告（ARCH-04 顺带并入的那件事）：renameReservedAllLangsTx 过去在**已发布分支的事务内**
// 用软口径再读一次语言清单，读失败就只迁移默认语言的保留路由 —— 而此时内核的 FS 激活已经完成，
// 事务照常提交，于是留下「访问面已切到新路径、其余语言的 reserved 行还在旧路径」的三方分裂。
//
// 修法：语言集合在**任何内核调用之前**按发布口径解析（读不到即失败），再作为参数传进编排名；
// 事务内不再读语言表。本用例用「工程服务未注入」这个最小复现形态验证这条防线：
// 解析必然失败，因此改 URL 必须在访问面**一个字节都没动**的前提下失败。

import (
	"context"
	"testing"

	"gorm.io/gorm"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
)

// newDegradedPageService 用同一 PG / 产物根再造一个 page 服务实例，但**不注入工程服务**。
//
// 这是「站点语言清单不可读」的最小可复现形态（生产里等价于工程服务缺失 / 降级装配）：
// 软口径（enabledLangsOf → pipeline.EnabledLangs）会静默退化成默认语言一种，
// 而发布口径（publishLangsOf → ResolveSiteLangs(..., Forbidden)）直接返回 ErrLangTableUnavailable。
func newDegradedPageService(t *testing.T, db *gorm.DB) pagecontract.PageService {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	return pageservice.NewService(
		pagemodel.NewPageModel(db),
		artifactservice.NewService(artifactmodel.NewArtifactModel(db)),
		pubservice.NewService(pubmodel.NewPublicationModel(db)),
		nil, // project：本用例要复现的正是「解析不到语言集合」
		blocks, nil, nil, nil, nil,
	)
}

// TestUpdateURLCannotMoveAccessFaceWithoutLanguageSet 语言集合解析不到时改 URL 必须失败，
// 且访问面 / 数据库都不留下半截改动。
func TestUpdateURLCannotMoveAccessFaceWithoutLanguageSet(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlain(t)
	twoLocaleProject(t, projects, projectID)

	pageID := createPage(t, svc, projectID, "/degraded/old", pageDocument).ID
	buildAndPublish(t, svc, pageID) // zh-CN 上线，旧路径已是访问面上的激活链接
	oldPath := "/degraded/old"
	newPath := "/degraded/new"
	// 建页登记两语言各一条 reserved；发布 zh-CN 时该语言的 reserved 被**原地升级**为
	// active（发布协议如此），所以此刻只剩 en-US 一条 reserved。
	reservedBefore := reservedPaths(t, db, projectID, pageID)
	if !equalStrings(reservedBefore, []string{"/en/degraded/old"}) {
		t.Fatalf("发布 zh-CN 后应只剩 en-US 的保留路由，实际 %v", reservedBefore)
	}

	degraded := newDegradedPageService(t, db)
	if _, err := degraded.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: pageID, NewPath: newPath}); err == nil {
		t.Fatal("站点语言集合不可读时改 URL 必须失败 —— 否则会带着不完整的语言集去切访问面")
	}

	// 访问面未动：新路径没有任何激活链接，旧路径仍指向原产物。
	if kind := activeKind(t, newPath); kind != "none" {
		t.Fatalf("改 URL 失败后新路径不得有激活链接，实际 %q", kind)
	}
	if kind := activeKind(t, oldPath); kind != "page" {
		t.Fatalf("改 URL 失败后旧路径应仍是激活页面，实际 %q", kind)
	}
	// 数据库未动：草稿路径 / 该语言激活路径 / 两语言保留路由都停在旧值。
	if got := pageColumnOf(t, db, pageID, "draft_path"); got != oldPath {
		t.Fatalf("改 URL 失败后 pages.draft_path 应保持 %q，实际 %q", oldPath, got)
	}
	if got := publicationOf(t, db, pageID, "zh-CN").ActivePath; got != oldPath {
		t.Fatalf("改 URL 失败后激活路径应保持 %q，实际 %q", oldPath, got)
	}
	if got := reservedPaths(t, db, projectID, pageID); !equalStrings(got, reservedBefore) {
		t.Fatalf("改 URL 失败后保留路由不得被迁移（半迁移的表现）：期望 %v，实际 %v", reservedBefore, got)
	}
	// 也没有留下任何回执：回执登记是改 URL 的**第一处持久化写**，位置在内核切访问面之前。
	// 语言集合解析不到时必须在它更早的地方失败 —— 若把语言集合留到后面（或事务内再读一次），
	// 这一步已经在目标路径上登记了一条 update_url 回执（即便随后被标 rolled_back），
	// 新老行为的差别就写在这一行上。
	if n := receiptsAt(t, db, newPath); n != 0 {
		t.Fatalf("改 URL 未开始就不该在目标路径留下任何回执，实际 %d 条", n)
	}
}

// receiptsAt 统计某路径上的全部回执行数（不按动作筛选）。
//
// 与 page_publish_ledger_test.go 的 receiptCount 区分：那个只数 switch_active，
// 本文件要断言的是「目标路径上什么都没有」，任何动作的回执都算越界。
func receiptsAt(t *testing.T, db *gorm.DB, path string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM publication_receipts WHERE path = ?", path).Scan(&n).Error; err != nil {
		t.Fatalf("统计回执失败: %v", err)
	}
	return n
}
