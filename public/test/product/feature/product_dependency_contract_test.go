package feature

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatadto "go_wp/internal/module/masterdata/dto"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/public/test/support"
)

// TestNewServiceRequiresAuditChanges 守住「审计是构造期必需依赖」：
// 少了它，写操作的变更记录会静默消失，那种配置必须在启动时就炸出来。
func TestNewServiceRequiresAuditChanges(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("审计依赖为 nil 时构造应 panic，否则写操作会静默丢审计")
		}
	}()
	projects := projectservice.NewService(projectmodel.NewProjectModel(nil))
	productservice.NewService(productmodel.NewModel(nil), projects, nil)
}

// failingChanges 让审计写入必然失败：用来证明「审计失败 = 整笔写操作回滚」。
type failingChanges struct{}

func (failingChanges) RecordChanges(context.Context, []*masterdatacontract.ChangeInput) error {
	return errors.New("审计写失败")
}

func (failingChanges) RecordChangesTx(context.Context, *gorm.DB, []*masterdatacontract.ChangeInput) error {
	return errors.New("审计写失败")
}

func (failingChanges) ListChanges(context.Context, *masterdatadto.ListChangeReq) ([]*masterdatadto.ChangeResp, error) {
	return nil, nil
}

func (failingChanges) CountChanges(context.Context, *masterdatadto.ListChangeReq) (int64, error) {
	return 0, nil
}

func (failingChanges) ListEntities(context.Context, *masterdatadto.ListEntityReq) ([]*masterdatadto.EntityHistoryResp, error) {
	return nil, nil
}

func (failingChanges) CountEntities(context.Context, *masterdatadto.ListEntityReq) (int64, error) {
	return 0, nil
}

func (failingChanges) EntityTimeline(context.Context, *masterdatadto.EntityTimelineReq) (*masterdatadto.EntityTimelineResp, error) {
	return nil, nil
}

func TestCreateRollsBackWhenAuditFails(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "审计回滚工程"})
	if err != nil {
		t.Fatalf("建工程失败：%v", err)
	}
	svc := productservice.NewService(productmodel.NewModel(db), projects, failingChanges{})
	price := 12.0
	if _, err := svc.Create(context.Background(), &productdto.CreateReq{
		ProjectID: project.ID, Name: "审计失败商品", Slug: "audit-fail", DefaultPrice: &price,
	}); err == nil {
		t.Fatal("审计失败必须让整笔创建打回，不能静默成功")
	}
	var n int64
	if err := db.Raw("SELECT count(*) FROM products WHERE project_id = ? AND slug = ?",
		project.ID, "audit-fail").Scan(&n).Error; err != nil {
		t.Fatalf("回查商品失败：%v", err)
	}
	if n != 0 {
		t.Fatalf("审计失败时商品不该落库，实际 %d 行", n)
	}
}

// TestCreateLeavesProductDraftWithoutInstance 守住「创建 ≠ 发布」：
// 新商品默认 draft，且创建不建网页实例 —— 网页发布是另一次显式动作。
func TestCreateLeavesProductDraftWithoutInstance(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		return
	}
	price := 20.0
	created, err := f.svc.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "只创建不发布", Slug: "create-not-publish", DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建失败：%v", err)
	}
	if created.Status != "draft" {
		t.Fatalf("新建商品状态应为 draft，实际 %q", created.Status)
	}
	var instances int64
	if err := f.db.Raw("SELECT count(*) FROM presentation_instances WHERE project_id = ? AND entity_type = 'product' AND entity_id = ?",
		f.projectID, created.ID).Scan(&instances).Error; err != nil {
		t.Fatalf("回查实例失败：%v", err)
	}
	if instances != 0 {
		t.Fatalf("创建不该生出网页实例（发布是另一件事），实际 %d 个", instances)
	}
}
