package feature

import (
	"errors"
	"reflect"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
)

// TestProductUpdateOptionalNumbers 区分局部更新中的缺失、零值和明确清空。
func TestProductUpdateOptionalNumbers(t *testing.T) {
	value, zero := 12.5, 0.0
	for _, tc := range []struct {
		name, input string
		want        *float64
	}{
		{"omitted", `{}`, &value},
		{"zero", `{"defaultPrice":0,"weight":0}`, &zero},
		{"clear", `{"clearDefaultPrice":true,"clearWeight":true}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if f == nil {
				t.Fatal("本专项必须使用真实 PostgreSQL")
			}
			value := 12.5
			p, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: "可选数值", DefaultPrice: &value, Weight: &value})
			if err != nil {
				t.Fatal(err)
			}
			var req productdto.UpdateReq
			if err := json.Unmarshal([]byte(tc.input), &req); err != nil {
				t.Fatal(err)
			}
			req.ID, req.ProjectID = p.ID, f.projectID
			if _, err := f.svc.Update(t.Context(), &req); err != nil {
				t.Fatal(err)
			}
			got, err := f.svc.Get(t.Context(), &productdto.GetReq{ID: p.ID, ProjectID: f.projectID})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.DefaultPrice, tc.want) || !reflect.DeepEqual(got.Weight, tc.want) {
				t.Fatalf("数值语义不符：price=%v weight=%v want=%v", got.DefaultPrice, got.Weight, tc.want)
			}
			if len(got.Variants) != 1 || got.Variants[0].Price != value {
				t.Fatalf("更新主体默认价不得改写已有变体价格：%+v", got.Variants)
			}
		})
	}
}

// TestProductUpdateRejectsAmbiguousClear 拒绝同时设置值和清空，且不留下部分写入。
func TestProductUpdateRejectsAmbiguousClear(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		t.Fatal("本专项必须使用真实 PostgreSQL")
	}
	p, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: "原名称"})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		`{"name":"不得写入","defaultPrice":0,"clearDefaultPrice":true}`,
		`{"name":"不得写入","weight":0,"clearWeight":true}`,
	} {
		var req productdto.UpdateReq
		if err := json.Unmarshal([]byte(input), &req); err != nil {
			t.Fatal(err)
		}
		req.ID, req.ProjectID = p.ID, f.projectID
		if _, err := f.svc.Update(t.Context(), &req); err == nil || err.Error() != productenums.ErrInvalidParam {
			t.Fatalf("有歧义的清空必须拒绝：%v", err)
		}
		got, err := f.svc.Get(t.Context(), &productdto.GetReq{ID: p.ID, ProjectID: f.projectID})
		if err != nil || got.Name != p.Name || got.DefaultPrice != nil || got.Weight != nil {
			t.Fatalf("拒绝请求仍写入主体：%+v %v", got, err)
		}
	}
}

// TestProductUpdateClearAuditsAndRollsBack 验证清空使用锁内事务，审计失败时整体回滚。
func TestProductUpdateClearAuditsAndRollsBack(t *testing.T) {
	f := newFixture(t)
	if f == nil {
		t.Fatal("本专项必须使用真实 PostgreSQL")
	}
	value := 12.5
	p, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: "清空回滚", DefaultPrice: &value, Weight: &value})
	if err != nil {
		t.Fatal(err)
	}
	var req productdto.UpdateReq
	if err := json.Unmarshal([]byte(`{"clearDefaultPrice":true,"clearWeight":true}`), &req); err != nil {
		t.Fatal(err)
	}
	req.ID, req.ProjectID = p.ID, f.projectID
	injected := errors.New("injected update audit failure")
	f.rewireProductChanges(t, &failingChangesPort{err: injected})
	if _, err := f.svc.Update(t.Context(), &req); !errors.Is(err, injected) {
		t.Fatalf("审计故障未返回：%v", err)
	}
	got, err := f.svc.Get(t.Context(), &productdto.GetReq{ID: p.ID, ProjectID: f.projectID})
	if err != nil || !reflect.DeepEqual(got.DefaultPrice, &value) || !reflect.DeepEqual(got.Weight, &value) {
		t.Fatalf("清空未随审计失败回滚：%+v %v", got, err)
	}

	changes := &capturedProductChanges{}
	f.rewireProductChanges(t, changes)
	fired := afterProductSnapshot(t, f.db, "products", func() error {
		return rls.InProjectScope(t.Context(), f.db, f.projectID, func(tx *gorm.DB) error {
			return tx.Model(&productmodel.ProductEntity{}).Where("id = ?", p.ID).Updates(map[string]any{"name": "并发名称", "default_price": 23.5, "weight": 23.5}).Error
		})
	})
	if _, err := f.svc.Update(t.Context(), &req); err != nil {
		t.Fatal(err)
	}
	got, err = f.svc.Get(t.Context(), &productdto.GetReq{ID: p.ID, ProjectID: f.projectID})
	if err != nil || !*fired || got.Name != "并发名称" || got.DefaultPrice != nil || got.Weight != nil {
		t.Fatalf("锁内清空或未改字段恢复错误：%+v %v fired=%t", got, err, *fired)
	}
	if len(changes.inputs) != 1 {
		t.Fatalf("清空应记录一次审计：%+v", changes.inputs)
	}
	change := changes.inputs[0]
	if change.Before["default_price"] != "23.50" || change.After["default_price"] != "" || change.Before["name"] != change.After["name"] {
		t.Fatalf("清空审计未使用锁内快照：before=%v after=%v", change.Before, change.After)
	}
}
