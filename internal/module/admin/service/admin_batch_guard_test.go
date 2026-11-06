package adminservice

import (
	"context"
	"errors"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
)

// TestAdminBatchSetDeptIDRejectsEmptySelection 空选择不是「改了 0 条」的成功。
//
// 它必须在碰数据库之前就拒绝：service 只持有 model 句柄，nil db 会在事务里 panic，
// 所以这里断言的是「没有走到事务」这件事本身。
func TestAdminBatchSetDeptIDRejectsEmptySelection(t *testing.T) {
	svc := NewService(nil)
	for _, req := range []*admindto.AdminBatchSetDeptIDReq{
		nil,
		{DeptID: 1, UserIDs: nil},
		{DeptID: 1, UserIDs: []uint64{}},
		// 全是 0 与重复：去重后仍然是 0 个有效用户。
		{DeptID: 1, UserIDs: []uint64{0, 0}},
	} {
		err := svc.AdminBatchSetDeptID(context.Background(), req)
		if err == nil {
			t.Fatalf("空选择应被拒绝：%+v", req)
		}
		fact, ok := facing.FactOf(err)
		if !ok || !fact.Business {
			t.Fatalf("空选择应是业务错误事实：%v", err)
		}
		want := adminenums.ErrAdminSelectionEmpty
		if req == nil {
			want = adminenums.MsgBadRequest
		}
		if fact.Key != want {
			t.Fatalf("空选择 key = %q，want %q", fact.Key, want)
		}
		if status, ok := facing.StatusOf(err); !ok || status != 400 {
			t.Fatalf("空选择应声明 400：%v", err)
		}
	}
}

// TestAdminBatchSetDeptIDRejectsOverLimit 去重后超过共享上限整批拒绝。
func TestAdminBatchSetDeptIDRejectsOverLimit(t *testing.T) {
	svc := NewService(nil)
	ids := make([]uint64, 0, bulkids.MaxIDs+1)
	for i := 1; i <= bulkids.MaxIDs+1; i++ {
		ids = append(ids, uint64(i))
	}
	err := svc.AdminBatchSetDeptID(context.Background(), &admindto.AdminBatchSetDeptIDReq{DeptID: 1, UserIDs: ids})
	if err == nil {
		t.Fatal("超限应被拒绝")
	}
	fact, ok := facing.FactOf(err)
	if !ok || fact.Key != adminenums.ErrAdminBatchTooMany {
		t.Fatalf("超限 key = %+v，want %q", fact, adminenums.ErrAdminBatchTooMany)
	}
	if errors.Is(err, adminenums.Err(adminenums.ErrAdminSelectionEmpty)) {
		t.Fatal("超限不能被当成空选择")
	}
}
