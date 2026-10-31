package productservice_test

// product_stock_failure_test.go — 库存列的四种事实必须彼此可辨（PRD-017 验收点）。
//
// 四种事实：全部仓跟踪（数量可为 0）／任一仓不跟踪（∞）／任何仓都没有行（未入库）／
// **本次读取失败**。前三种是「查出来的结论」，第四种是「没查成」—— 从前它退化成
// 未入库（三态零值 + 一个字都不记），现场看起来像「数据被清空了」。
//
// 本用例只钉住**事实可辨**：故障时 StockUnknown 为 true、三态保持零值；
// 显示成什么文案属 DEC-02 待定，模板本批不改，故这里不断言任何可见文案。

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

// errStockReadInjected 注入给库存端口的读失败。
var errStockReadInjected = errors.New("injected: 库存读失败")

// stockErrorPortStub 恒失败的库存端口（模拟库存侧故障）。
type stockErrorPortStub struct {
	inventorycontract.ProductStockPort
	calls int
}

func (s *stockErrorPortStub) WarehouseStocksByProducts(context.Context, string, []string) ([]inventorydto.ProductWarehouseStock, error) {
	s.calls++
	return nil, errStockReadInjected
}

func TestStockFactsDistinguishFailureFromEmpty(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "库存事实")
	fx := seedParityFixture(t, db, projectID)
	ctx := context.Background()

	newService := func(port inventorycontract.ProductStockPort) *productservice.Service {
		s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
			rows: []projectdto.ProjectResp{{ID: projectID, Name: "库存事实"}},
		})
		s.SetWarehouseOptionsPort(parityWarehousePortStub{
			options: []productdto.WarehouseOptionResp{{ID: parityWarehouseID, Name: "主仓"}},
		})
		if port != nil {
			s.SetInventoryService(port)
		}
		return s
	}
	query := func(t *testing.T, s *productservice.Service) map[string]*productdto.ProductResp {
		t.Helper()
		result, err := s.QueryProducts(ctx, &productdto.ListReq{ProjectID: projectID})
		if err != nil {
			t.Fatalf("列表查询失败：%v", err)
		}
		byID := make(map[string]*productdto.ProductResp, len(result.Items))
		for _, item := range result.Items {
			byID[item.ID] = item
		}
		return byID
	}

	t.Run("全部跟踪且数量为零记为 tracked 0，不是未入库", func(t *testing.T) {
		items := query(t, newService(parityStockPortStub{rows: []inventorydto.ProductWarehouseStock{
			{ProductID: fx.variantProduct, WarehouseID: parityWarehouseID, WarehouseName: "主仓", TrackQuantity: true, Quantity: 0},
		}}))
		item := items[fx.variantProduct]
		if item == nil {
			t.Fatal("变体商品缺失")
		}
		if item.StockState != productenums.StockStateTracked || item.StockTotal != 0 || item.StockUnknown {
			t.Fatalf("零库存应是 tracked/0 且非未知，实际 %+v", item)
		}
	})

	t.Run("任一仓不跟踪记为 infinite，不求和", func(t *testing.T) {
		items := query(t, newService(parityStockPortStub{rows: []inventorydto.ProductWarehouseStock{
			{ProductID: fx.variantProduct, WarehouseID: parityWarehouseID, WarehouseName: "主仓", TrackQuantity: false, Quantity: 3},
			{ProductID: fx.variantProduct, WarehouseID: "22222222-2222-2222-2222-222222222222", WarehouseName: "副仓", TrackQuantity: true, Quantity: 5},
		}}))
		item := items[fx.variantProduct]
		if item == nil {
			t.Fatal("变体商品缺失")
		}
		if item.StockState != productenums.StockStateInfinite || item.StockTotal != 0 || item.StockUnknown {
			t.Fatalf("混合状态应记 infinite 且不求和，实际 %+v", item)
		}
	})

	t.Run("没有任何库存行记为 none", func(t *testing.T) {
		items := query(t, newService(parityStockPortStub{rows: []inventorydto.ProductWarehouseStock{}}))
		item := items[fx.singleProduct]
		if item == nil {
			t.Fatal("单品商品缺失")
		}
		if item.StockState != productenums.StockStateNone || item.StockUnknown {
			t.Fatalf("零行应是 none 且非未知，实际 %+v", item)
		}
	})

	t.Run("读取失败标记未知且三态保持零值", func(t *testing.T) {
		port := &stockErrorPortStub{}
		items := query(t, newService(port))
		if port.calls != 1 {
			t.Fatalf("库存端口应恰好被调用 1 次（整页一次批量读），实际 %d", port.calls)
		}
		if len(items) != 2 {
			t.Fatalf("库存故障不得吞掉商品行，实际 %d 行", len(items))
		}
		for id, item := range items {
			if !item.StockUnknown {
				t.Fatalf("商品 %s 的库存读取失败必须标记未知：%+v", id, item)
			}
			if item.StockState != "" || item.StockTotal != 0 {
				t.Fatalf("故障时不得伪造三态结论：%+v", item)
			}
		}
	})

	t.Run("未注入库存端口不算故障", func(t *testing.T) {
		items := query(t, newService(nil))
		for id, item := range items {
			if item.StockUnknown {
				t.Fatalf("没装库存依赖是「不投影」，不是「读失败」：%s %+v", id, item)
			}
			if item.StockState != "" {
				t.Fatalf("没装依赖时三态应保持零值：%s %+v", id, item)
			}
		}
	})
}
