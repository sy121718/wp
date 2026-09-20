// product_related_ids_test.go — related_ids 写路径的引用校验（审计 DB-03 §1.2 / PROD-01）。
//
// 缺陷形态：分类 / 标签 / 属性三条引用面都有「存在 + 同工程」的服务层校验，只有
// related_ids（products 表的**同表自引用**）原样落库 —— 任意 uuid 都能写进 JSONB，
// 而 JSON 数组成员没有任何数据库级外键兜底，写脏了只能靠人工对账发现。
//
// 本用例钉住四件事（缺任何一条都会让「相关商品」出现悬空 id 或环）：
//  1. 引用不存在的商品 / 跨工程的商品 / 商品自己 → 拒绝；
//  2. 拒绝时**一次列全**全部非法条目（id + 数量 + 跨工程条目的归属工程）；
//  3. 拒绝是整体的：不许静默丢弃非法项后把剩下的写进去；
//  4. 合法引用去重后落库（同一次提交写出同一份数组）。
package feature

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

// secondProject 再建一个工程：跨工程引用场景需要「另一个工程的商品」。
func (f *fixture) secondProject(t *testing.T) string {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	p, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "相关商品测试工程B"})
	if err != nil {
		t.Fatalf("创建第二个测试工程失败: %v", err)
	}
	return p.ID
}

// mkRelatedProduct 建一个不带相关商品引用的商品（引用校验的「合法目标」）。
func (f *fixture) mkRelatedProduct(t *testing.T, projectID, name, slug string) *productdto.ProductResp {
	t.Helper()
	res, err := f.svc.Create(context.Background(), &productdto.CreateReq{
		ProjectID: projectID, Name: name, Slug: slug,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	return res
}

// relatedIDsOf 读回商品的 related_ids（走真实读路径，不是内存里的实体）。
func (f *fixture) relatedIDsOf(t *testing.T, projectID, id string) []string {
	t.Helper()
	res, err := f.svc.Get(context.Background(), &productdto.GetReq{ProjectID: projectID, ID: id})
	if err != nil {
		t.Fatalf("读商品 %s 失败: %v", id, err)
	}
	return res.RelatedIDs
}

// requireRelatedRejected 断言拒绝错误能定位到给定片段（并属于 ErrRelatedInvalid）。
func requireRelatedRejected(t *testing.T, err error, wantFragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("非法 related_ids 应被拒绝，实际写入成功")
	}
	msg := err.Error()
	if !strings.Contains(msg, productenums.ErrRelatedInvalid) {
		t.Fatalf("拒绝应回带 %s，实际 %v", productenums.ErrRelatedInvalid, err)
	}
	for _, frag := range wantFragments {
		if !strings.Contains(msg, frag) {
			t.Fatalf("拒绝信息应能定位到 %q，实际 %v", frag, err)
		}
	}
}

// TestCreateRelatedIDsRejectsMissingReference 新建时引用不存在的商品 → 拒绝且商品未创建。
func TestCreateRelatedIDsRejectsMissingReference(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	ghost := uuid.NewString()
	_, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "悬空引用商品", Slug: "rel-ghost",
		RelatedIDs: []string{ghost},
	})
	requireRelatedRejected(t, err, ghost)

	list, lerr := f.svc.List(ctx, &productdto.ListReq{ProjectID: f.projectID})
	if lerr != nil {
		t.Fatalf("列表查询失败: %v", lerr)
	}
	if len(list) != 0 {
		t.Fatalf("校验失败的商品不该落库，实际列表有 %d 个商品", len(list))
	}
}

// TestUpdateRelatedIDsRejectsMissingReference 更新时引用不存在的商品 → 拒绝且原引用不变。
func TestUpdateRelatedIDsRejectsMissingReference(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := f.mkRelatedProduct(t, f.projectID, "主商品", "rel-main")
	ghost := uuid.NewString()
	_, err := f.svc.Update(ctx, &productdto.UpdateReq{
		ID: p.ID, ProjectID: f.projectID, RelatedIDs: []string{ghost},
	})
	requireRelatedRejected(t, err, ghost)
	if got := f.relatedIDsOf(t, f.projectID, p.ID); len(got) != 0 {
		t.Fatalf("拒绝后原 related_ids 应保持不变（空），实际 %v", got)
	}
}

// TestRelatedIDsRejectsCrossProjectReference 跨工程引用 → 拒绝，且错误里带上对方工程 id。
func TestRelatedIDsRejectsCrossProjectReference(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	otherProject := f.secondProject(t)
	mine := f.mkRelatedProduct(t, f.projectID, "本工程商品", "rel-mine")
	theirs := f.mkRelatedProduct(t, otherProject, "别的工程商品", "rel-theirs")

	_, err := f.svc.Update(ctx, &productdto.UpdateReq{
		ID: mine.ID, ProjectID: f.projectID, RelatedIDs: []string{theirs.ID},
	})
	// 跨工程条目必须带上对方工程 id：光有商品 id，运营找不到人解除。
	requireRelatedRejected(t, err, theirs.ID, otherProject)

	if got := f.relatedIDsOf(t, f.projectID, mine.ID); len(got) != 0 {
		t.Fatalf("跨工程引用被拒后不该写入，实际 %v", got)
	}
}

// TestRelatedIDsRejectsSelfReference 相关商品不能指向自己（自引用会让「相关商品」成环）。
func TestRelatedIDsRejectsSelfReference(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := f.mkRelatedProduct(t, f.projectID, "自引用商品", "rel-self")
	_, err := f.svc.Update(ctx, &productdto.UpdateReq{
		ID: p.ID, ProjectID: f.projectID, RelatedIDs: []string{p.ID},
	})
	requireRelatedRejected(t, err, p.ID, "不能指向自己")
}

// TestRelatedIDsReportsAllInvalidAtOnce 三类问题一次列全，且整批拒绝（不部分写入）。
func TestRelatedIDsReportsAllInvalidAtOnce(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	otherProject := f.secondProject(t)
	mine := f.mkRelatedProduct(t, f.projectID, "批量校验商品", "rel-batch")
	theirs := f.mkRelatedProduct(t, otherProject, "别的工程商品", "rel-batch-theirs")
	ghostA, ghostB := uuid.NewString(), uuid.NewString()

	_, err := f.svc.Update(ctx, &productdto.UpdateReq{
		ID: mine.ID, ProjectID: f.projectID,
		RelatedIDs: []string{ghostA, theirs.ID, ghostB, mine.ID},
	})
	requireRelatedRejected(t, err, ghostA, ghostB, theirs.ID, mine.ID, otherProject)

	// 「一次列全」另外一面：非法项一个都不许被静默丢弃后把其余的写进去。
	if got := f.relatedIDsOf(t, f.projectID, mine.ID); len(got) != 0 {
		t.Fatalf("整批拒绝后不该写入任何引用，实际 %v", got)
	}
}

// TestRelatedIDsDedupAndPersist 合法引用去重后落库（保序），读回来与提交的顺序一致。
func TestRelatedIDsDedupAndPersist(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	first := f.mkRelatedProduct(t, f.projectID, "相关商品一", "rel-one")
	second := f.mkRelatedProduct(t, f.projectID, "相关商品二", "rel-two")
	mine := f.mkRelatedProduct(t, f.projectID, "主商品", "rel-host")

	res, err := f.svc.Update(ctx, &productdto.UpdateReq{
		ID: mine.ID, ProjectID: f.projectID,
		// 重复 + 空白：去重后应只剩两个 id，且保持首次出现的顺序。
		RelatedIDs: []string{first.ID, " ", first.ID, second.ID},
	})
	if err != nil {
		t.Fatalf("合法相关商品引用不该被拒: %v", err)
	}
	want := []string{first.ID, second.ID}
	if len(res.RelatedIDs) != len(want) || res.RelatedIDs[0] != want[0] || res.RelatedIDs[1] != want[1] {
		t.Fatalf("related_ids 应为去重保序的 %v，实际 %v", want, res.RelatedIDs)
	}
	if got := f.relatedIDsOf(t, f.projectID, mine.ID); len(got) != 2 {
		t.Fatalf("落库的 related_ids 应有 2 个，实际 %v", got)
	}
}

// TestCreateRelatedIDsAcceptsSameProjectReferences 新建路径的合法引用照常落库（校验不放宽为拒绝一切）。
func TestCreateRelatedIDsAcceptsSameProjectReferences(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	target := f.mkRelatedProduct(t, f.projectID, "被引用商品", "rel-target")
	res, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "新建带相关商品", Slug: "rel-created",
		RelatedIDs: []string{target.ID},
	})
	if err != nil {
		t.Fatalf("同工程合法引用不该被拒: %v", err)
	}
	if len(res.RelatedIDs) != 1 || res.RelatedIDs[0] != target.ID {
		t.Fatalf("related_ids 应落库 %s，实际 %v", target.ID, res.RelatedIDs)
	}
}
