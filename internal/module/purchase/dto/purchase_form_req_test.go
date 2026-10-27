package purchasedto

import "testing"

// 登记收货的表单是三列同名数组（lineId / quantity / unitPrice），按序号读。
//
// 这里钉的是**整单一次提交**的解析口径：本次到货量 0 / 空的行整行跳过
// （0 不是「收 0 件」，是「这行本次不收」），跳过之后不能错位到别的行。
func TestPurchaseReceiptFormReqRegisterReq(t *testing.T) {
	cases := []struct {
		name string
		form PurchaseReceiptFormReq
		want []ReceiptLineReq
	}{
		{
			name: "整单多行各自数量",
			form: PurchaseReceiptFormReq{
				ProjectID: " P1 ", OrderID: " O1 ", RequestID: " R1 ",
				LineID:   []string{"L1", "L2"},
				Quantity: []string{"3", "4"},
			},
			want: []ReceiptLineReq{{LineID: "L1", Quantity: 3}, {LineID: "L2", Quantity: 4}},
		},
		{
			name: "数量为 0 的行整行跳过（不错位）",
			form: PurchaseReceiptFormReq{
				OrderID:   "O1",
				LineID:    []string{"L1", "L2", "L3"},
				Quantity:  []string{"0", "5", ""},
				UnitPrice: []string{"", "2.5", ""},
			},
			want: func() []ReceiptLineReq {
				price := 2.5
				return []ReceiptLineReq{{LineID: "L2", Quantity: 5, UnitPrice: &price}}
			}(),
		},
		{
			name: "行号为空的脏行跳过",
			form: PurchaseReceiptFormReq{
				OrderID:  "O1",
				LineID:   []string{"", "L2"},
				Quantity: []string{"7", "1"},
			},
			want: []ReceiptLineReq{{LineID: "L2", Quantity: 1}},
		},
		{
			name: "一格没填 → 空行集（由服务层报没有可收货的行）",
			form: PurchaseReceiptFormReq{
				OrderID:  "O1",
				LineID:   []string{"L1"},
				Quantity: []string{"0"},
			},
			want: nil,
		},
		{
			name: "数量列比行号列短（表单缺列）不 panic",
			form: PurchaseReceiptFormReq{
				OrderID:  "O1",
				LineID:   []string{"L1", "L2"},
				Quantity: []string{"2"},
			},
			want: []ReceiptLineReq{{LineID: "L1", Quantity: 2}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.form.RegisterReq("op")
			if len(got.Lines) != len(tc.want) {
				t.Fatalf("行数 = %d，want %d：%+v", len(got.Lines), len(tc.want), got.Lines)
			}
			for i := range tc.want {
				if got.Lines[i].LineID != tc.want[i].LineID || got.Lines[i].Quantity != tc.want[i].Quantity {
					t.Fatalf("第 %d 行 = %+v，want %+v", i, got.Lines[i], tc.want[i])
				}
				switch {
				case tc.want[i].UnitPrice == nil && got.Lines[i].UnitPrice != nil:
					t.Fatalf("第 %d 行不该有到货价，实际 %v", i, *got.Lines[i].UnitPrice)
				case tc.want[i].UnitPrice != nil && got.Lines[i].UnitPrice == nil:
					t.Fatalf("第 %d 行缺少到货价 %v", i, *tc.want[i].UnitPrice)
				case tc.want[i].UnitPrice != nil && *got.Lines[i].UnitPrice != *tc.want[i].UnitPrice:
					t.Fatalf("第 %d 行到货价 = %v，want %v", i, *got.Lines[i].UnitPrice, *tc.want[i].UnitPrice)
				}
			}
			if got.OrderID != "O1" {
				t.Fatalf("单号应去掉首尾空白，实际 %q", got.OrderID)
			}
			if got.OperatorID != "op" {
				t.Fatalf("操作人应透传，实际 %q", got.OperatorID)
			}
		})
	}
}

// 建单表单的草稿勾选：checkbox 未勾不提交 → false；未勾即「直接下单」。
func TestPurchaseCreateFormReqAsDraft(t *testing.T) {
	base := PurchaseCreateFormReq{
		ProjectID: "P1", Code: "PO-1", SourceID: "S1",
		LineSku: []string{"V1|P|TEE1"}, LineQuantity: []string{"2"}, LineUnitPrice: []string{"1.5"},
	}
	if got := base.CreateReq("op"); got.AsDraft {
		t.Fatalf("未勾选草稿时 AsDraft 应为 false")
	}
	base.AsDraft = true
	got := base.CreateReq("op")
	if !got.AsDraft {
		t.Fatalf("勾选草稿后 AsDraft 应为 true")
	}
	if len(got.Lines) != 1 || got.Lines[0].VariantID != "V1" || got.Lines[0].Quantity != 2 {
		t.Fatalf("采购行解析错位：%+v", got.Lines)
	}
}
