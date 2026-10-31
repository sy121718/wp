package feature

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

func (f *fixture) secondProject(t *testing.T) string {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	p, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "相关商品测试工程B"})
	if err != nil {
		t.Fatalf("创建第二个测试工程失败: %v", err)
	}
	return p.ID
}

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

func (f *fixture) relatedIDsOf(t *testing.T, projectID, id string) []string {
	t.Helper()
	res, err := f.svc.Get(context.Background(), &productdto.GetReq{ProjectID: projectID, ID: id})
	if err != nil {
		t.Fatalf("读商品 %s 失败: %v", id, err)
	}
	return res.RelatedIDs
}

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

	requireRelatedRejected(t, err, theirs.ID, otherProject)

	if got := f.relatedIDsOf(t, f.projectID, mine.ID); len(got) != 0 {
		t.Fatalf("跨工程引用被拒后不该写入，实际 %v", got)
	}
}

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

	requireRelatedRejected(t, err, p.ID, productenums.DetailRelatedSelfReference)
}

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

	if got := f.relatedIDsOf(t, f.projectID, mine.ID); len(got) != 0 {
		t.Fatalf("整批拒绝后不该写入任何引用，实际 %v", got)
	}
}

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

func TestRelatedOwnerLookupStaysScoped(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	otherProject := f.secondProject(t)
	mine := f.mkRelatedProduct(t, f.projectID, "归属甲", "owner-a")
	theirs := f.mkRelatedProduct(t, otherProject, "归属乙", "owner-b")
	ghost := uuid.NewString()

	owners, err := productmodel.NewModel(f.db).LookupIDOwners(ctx, []string{f.projectID, otherProject}, []string{mine.ID, theirs.ID, ghost, "  ", theirs.ID, ""})
	if err != nil {
		t.Fatalf("归属诊断不该失败: %v", err)
	}
	if got := owners[mine.ID]; got != f.projectID {
		t.Fatalf("本工程商品归属应为 %s，实际 %q", f.projectID, got)
	}
	if got := owners[theirs.ID]; got != otherProject {
		t.Fatalf("别工程商品归属应为 %s，实际 %q", otherProject, got)
	}
	if _, ok := owners[ghost]; ok {
		t.Fatalf("不存在的 id 不该有归属: %v", owners)
	}
	if len(owners) != 2 {
		t.Fatalf("归属结论应只有两条（空白与重复不产生条目），实际 %v", owners)
	}
}

func TestRelatedOwnerLookupEmptyInputSkipsQuery(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	owners, err := productmodel.NewModel(f.db).LookupIDOwners(context.Background(), []string{f.projectID}, nil)
	if err != nil {
		t.Fatalf("空输入不该报错: %v", err)
	}
	if len(owners) != 0 {
		t.Fatalf("空输入不该有归属结论，实际 %v", owners)
	}
}

func TestRelatedIDsCrossProjectDetailNamesOwnerProject(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	otherProject := f.secondProject(t)
	mine := f.mkRelatedProduct(t, f.projectID, "本工程商品", "rel-detail-mine")
	theirs := f.mkRelatedProduct(t, otherProject, "别工程商品", "rel-detail-theirs")
	_, err := f.svc.Update(ctx, &productdto.UpdateReq{
		ID: mine.ID, ProjectID: f.projectID, RelatedIDs: []string{theirs.ID},
	})
	requireRelatedRejected(t, err, theirs.ID+"@"+otherProject, productenums.DetailRelatedCrossProject)

	if msg := err.Error(); strings.Contains(msg, productenums.DetailRelatedMissing) {
		t.Fatalf("跨工程条目不该被判成不存在: %v", err)
	}
}
