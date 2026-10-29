package feature

// 同一订单项的重复明细不得放大退款；服务入口已按严格输入契约拒绝重复ID。
// 保留原漏洞样本，并证明拒绝未写任何退货单、单行额度校验和合法申请均有效。

import (
	"context"
	"strings"
	"testing"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/facing"
)

func TestCreateReturnRejectsDuplicateLinesForSameItem(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, variantID := f.addProduct(t, "退货重复行商品", 30, 20)
	orderID, userID, itemID := f.paidOrder(t, "return-duplicate@example.test", variantID, 2)
	countReturns := func() int64 {
		t.Helper()
		var count int64
		if err := f.db.Model(&ordermodel.ReturnEntity{}).Where("order_id = ?", orderID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	before := countReturns()
	req := &orderdto.ReturnRequestReq{
		ProjectID: f.projectID, OrderID: orderID, UserID: userID, Reason: "重复行放大测试",
		Items: []orderdto.ReturnItemReq{
			{OrderItemID: itemID, Quantity: 2},
			{OrderItemID: itemID, Quantity: 2},
		},
	}
	_, err := f.orders.RequestReturn(ctx, req)
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business || fact.Key != orderenums.ErrInvalidParam {
		t.Fatalf("重复订单项必须被严格输入校验拒绝，实际 %+v / %v", fact, err)
	}
	if got := countReturns(); got != before {
		t.Fatalf("重复行拒绝必须零写入: %d != %d", got, before)
	}

	// 独立核额度规则：合法明细ID的一行4件也不能越过可退2件。
	req.Items = []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 4}}
	_, err = f.orders.RequestReturn(ctx, req)
	fact, ok = facing.FactOf(err)
	if !ok || !fact.Business || fact.Key != orderenums.ErrReturnQuantityExceeded || !strings.Contains(fact.Tail, "退货重复行商品") {
		t.Fatalf("单行超量必须返回带商品明细的业务错误，实际 %+v / %v", fact, err)
	}
	if got := countReturns(); got != before {
		t.Fatalf("超量拒绝必须零写入: %d != %d", got, before)
	}

	// 反证：同一个订单合法的一行2件仍能正常申请。
	req.Items = []orderdto.ReturnItemReq{{OrderItemID: itemID, Quantity: 2}}
	if _, err := f.orders.RequestReturn(ctx, req); err != nil {
		t.Fatalf("合法申请应通过: %v", err)
	}
	if got := countReturns(); got != before+1 {
		t.Fatalf("合法申请只能新增一单: %d != %d", got, before+1)
	}
}
