package feature

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/pkg/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

func TestBusinessPresetGlobalReferencesPreviewRealContents(t *testing.T) {
	env := newRefEnv(t)
	env.blocks.(interface {
		SetPresetResolver(blockcontract.PresetResolver)
	}).SetPresetResolver(builder.BusinessPresetRegistry{})
	block, err := env.blocks.Create(t.Context(), &blockdto.CreateReq{ProjectID: env.projectID, Name: "共享购物流程", PresetKey: "commerce.cart-checkout", PresetVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := env.blocks.Detail(t.Context(), &blockdto.DetailReq{ProjectID: env.projectID, ID: block.ID})
	if err != nil {
		t.Fatalf("预设创建后明细不可读: %v", err)
	}
	if _, err := builder.ParsePage(loaded.Document); err != nil {
		t.Fatalf("预设创建后文档不可解析: %v", err)
	}

	legacy := regexp.MustCompile(`"kind"\s*:\s*"(instance|local)"`).ReplaceAllString(string(loaded.Document), `"kind":"$1","newTab":false`)
	if legacy == string(loaded.Document) {
		t.Fatal("夹具未形成历史错误形状")
	}
	if err := env.db.Table("blocks").Where("id = ?", block.ID).Update("document", legacy).Error; err != nil {
		t.Fatal(err)
	}
	stored, err := env.blocks.Detail(t.Context(), &blockdto.DetailReq{ProjectID: env.projectID, ID: block.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builder.ParsePage(stored.Document); err == nil {
		t.Fatal("历史显式跨kind字段必须仍被拒绝")
	}

	repaired, err := env.blocks.Update(t.Context(), &blockdto.UpdateReq{ID: stored.ID, Name: stored.Name, Kind: stored.Kind, Category: stored.Category, ReuseMode: stored.ReuseMode, ExpectedVersion: uint64(stored.Version), Document: loaded.Document})
	if err != nil || repaired.ID != block.ID || repaired.Version != loaded.Version+1 {
		t.Fatalf("修正未沿原ID版本化保存: %+v %v", repaired, err)
	}
	stored, err = env.blocks.Detail(t.Context(), &blockdto.DetailReq{ProjectID: env.projectID, ID: block.ID})
	if err != nil || stored.Name != loaded.Name || string(stored.Document) != string(loaded.Document) {
		t.Fatalf("修正改变作者内容或名称: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(env.db))
	pages := pageservice.NewService(pagemodel.NewPageModel(env.db), nil, nil, projects, env.blocks, nil, nil, nil, nil)
	pages.SetCheckoutCountries(func(context.Context, string) []core.CheckoutCountry {
		return []core.CheckoutCountry{{Code: "CN", Label: "中国"}}
	})
	document := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"first","type":"core.globalref","props":{"blockId":%q}},{"id":"second","type":"core.globalref","props":{"blockId":%q}}]}`, block.ID, block.ID)
	html, err := pages.CompilePreview(t.Context(), []byte(document), env.projectID, "/business-preview", "zh-CN", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first-b-commerce-cart", "first-b-commerce-checkout", "second-b-commerce-cart", "second-b-commerce-checkout"} {
		if !strings.Contains(string(html), `data-component-instance="`+id+`"`) {
			t.Fatalf("预设引用未展开%s，HTML=%s", id, html)
		}
	}
}
