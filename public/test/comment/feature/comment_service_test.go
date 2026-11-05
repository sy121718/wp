package commentfeature

import (
	"context"
	"strings"
	"testing"

	"gorm.io/gorm"

	commentcontract "go_wp/internal/module/comment/contract"
	commentdto "go_wp/internal/module/comment/dto"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
	commentservice "go_wp/internal/module/comment/service"
	"go_wp/public/test/support"
)

const testProjectID = "33333333-3333-4333-8333-333333333333"

func commentTypes() []commentcontract.EntityType {
	return []commentcontract.EntityType{
		{Type: "article", Label: commentcontract.LabelPair{Key: "admin.article.list.heading", Fallback: "文章"}},
		{Type: "product", Label: commentcontract.LabelPair{Key: "admin.product_translations.entityType.product", Fallback: "商品"}},
	}
}

func setupCommentService(t *testing.T) (*commentservice.Service, *gorm.DB) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	return commentservice.NewService(commentmodel.NewModel(db), commentTypes()), db
}

func submitAndApprove(t *testing.T, svc *commentservice.Service, entityType, entityID, body string, userID uint64) int64 {
	t.Helper()
	res, err := svc.Submit(context.Background(), &commentdto.SubmitReq{ProjectID: testProjectID, EntityType: entityType, EntityID: entityID, UserID: userID, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != commentenums.StatusPending {
		t.Fatalf("新评论必须先审后发: %+v", res)
	}
	receipt := reviewViaAPI(t, svc, res.ID, commentenums.StatusApproved, 9)
	if !strings.Contains(receipt, "1") {
		t.Fatalf("审核回执需与实际一条写入相等: %s", receipt)
	}
	return res.ID
}

func approvedComments(t *testing.T, svc *commentservice.Service, entityType, entityID string) *commentdto.ListResp {
	t.Helper()
	res, err := svc.ListApproved(context.Background(), &commentdto.ListReq{ProjectID: testProjectID, EntityType: entityType, EntityID: entityID})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestCommentServiceHidesUnapprovedComment(t *testing.T) {
	svc, _ := setupCommentService(t)
	approved := submitAndApprove(t, svc, "article", "post-1", "已审核正文", 41)
	pending, err := svc.Submit(context.Background(), &commentdto.SubmitReq{ProjectID: testProjectID, EntityType: "article", EntityID: "post-1", UserID: 42, Body: "待审核正文"})
	if err != nil {
		t.Fatal(err)
	}
	list := approvedComments(t, svc, "article", "post-1")
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != approved || list.Items[0].ID == pending.ID {
		t.Fatalf("只应返回审核API已批准的那条: %+v", list)
	}
}

func TestCommentServiceIsolatesEntities(t *testing.T) {
	svc, _ := setupCommentService(t)
	articleA := submitAndApprove(t, svc, "article", "post-A", "文章A", 51)
	articleB := submitAndApprove(t, svc, "article", "post-B", "文章B", 51)
	productA := submitAndApprove(t, svc, "product", "post-A", "商品A", 51)
	for _, tc := range []struct {
		kind, id string
		want     int64
	}{{"article", "post-A", articleA}, {"article", "post-B", articleB}, {"product", "post-A", productA}} {
		list := approvedComments(t, svc, tc.kind, tc.id)
		if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != tc.want {
			t.Fatalf("实体隔离不成立 %s/%s: %+v", tc.kind, tc.id, list)
		}
	}
}

func TestCommentServiceRejectsUnregisteredEntityWithoutWriting(t *testing.T) {
	svc, db := setupCommentService(t)
	if _, err := svc.Submit(context.Background(), &commentdto.SubmitReq{ProjectID: testProjectID, EntityType: "order", EntityID: "o-1", UserID: 61, Body: "未注册类型"}); err == nil {
		t.Fatal("未注册类型必须拒绝")
	}
	var n int64
	if err := db.Model(&commentmodel.Entity{}).Where("entity_type = ?", "order").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("被拒的评论不能落库，实际%d条", n)
	}
}

func TestCommentServiceRateLimitPreventsFurtherWrites(t *testing.T) {
	svc, _ := setupCommentService(t)
	accepted := 0
	blocked := false
	for i := 0; i < 12; i++ {
		_, err := svc.Submit(context.Background(), &commentdto.SubmitReq{ProjectID: testProjectID, EntityType: "article", EntityID: "post-rate", UserID: 71, Body: "连续提交"})
		if err == nil {
			accepted++
			continue
		}
		if err.Error() != commentenums.ErrRateLimited {
			t.Fatalf("拒绝原因应为限流: %v", err)
		}
		blocked = true
		break
	}
	list, err := svc.AdminList(context.Background(), &commentdto.AdminListReq{ProjectID: testProjectID, EntityType: "article", PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if !blocked || list.Total != int64(accepted) || len(list.Items) != accepted {
		t.Fatalf("限流后不能继续写入: blocked=%v accepted=%d total=%d", blocked, accepted, list.Total)
	}
}

func TestCommentListAndSubmitShareOneProjectScope(t *testing.T) {
	svc, _ := setupCommentService(t)
	created, err := svc.Submit(context.Background(), &commentdto.SubmitReq{ProjectID: testProjectID, EntityType: "article", EntityID: "post-scope", UserID: 88, Body: "作用域检查"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := svc.AdminList(context.Background(), &commentdto.AdminListReq{ProjectID: testProjectID, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("同工程写入与审核队列不等: %+v", list)
	}
	other, err := svc.AdminList(context.Background(), &commentdto.AdminListReq{ProjectID: "44444444-4444-4444-8444-444444444444", PageSize: 10})
	if err != nil || other.Total != 0 || len(other.Items) != 0 {
		t.Fatalf("另一工程不能读入本工程评论: %+v %v", other, err)
	}
}
