package productservice_test

import (
	"context"
	"errors"
	"testing"

	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/facing"
)

func TestDetailTemplateWriteValidatesBeforeUsingDependencies(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func() error
		key  string
	}{
		{"空白模板名", func() error {
			return productservice.CreateDetailTemplate(context.Background(), productservice.DetailTemplateDeps{}, "p", "  ", "")
		}, productenums.ErrDetailTemplateNameRequired},
		{"空白路径", func() error {
			return productservice.UpdateDetailTemplateURL(context.Background(), nil, "p", "id", "  ", true)
		}, productenums.ErrDetailTemplatePathRequired},
		{"空白实例", func() error {
			return productservice.ReapplyDetailTemplatePreset(context.Background(), nil, &productdto.DetailTemplateModeReapplyReq{InstanceID: " "})
		}, productenums.ErrInvalidParam},
		{"空白快照", func() error {
			return productservice.RollbackDetailTemplateDocument(context.Background(), nil, &productdto.DetailTemplateModeRollbackReq{InstanceID: "id", SnapshotID: " "})
		}, productenums.ErrInvalidParam},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fact, ok := facing.FactOf(tc.run())
			if !ok || fact.Key != tc.key {
				t.Fatalf("错误事实=%+v，want %s", fact, tc.key)
			}
		})
	}
}

func TestProductPreviewRejectsEmptyArtifacts(t *testing.T) {
	for _, res := range []*presentationcontract.PreviewInstanceResp{nil, {}, {HTML: " \n "}} {
		got, err := productservice.PreviewDetailTemplate(context.Background(), &fakeDetailInstances{previewResp: res}, "project", "product", "template")
		status, _ := facing.StatusOf(err)
		if got != nil || status != 500 {
			t.Fatalf("空构建结果必须失败：result=%+v status=%d err=%v", got, status, err)
		}
	}
}

type previewBindingReader struct {
	bound                productcontract.BoundTemplateView
	err                  error
	projectID, productID string
}

func (r *previewBindingReader) QueryBoundDetailTemplate(_ context.Context, projectID, productID string) (productcontract.BoundTemplateView, error) {
	r.projectID, r.productID = projectID, productID
	return r.bound, r.err
}

func TestSavedProductPreviewUsesBoundInstanceAndResolvedProject(t *testing.T) {
	products := &fakeDetailProducts{t: t, get: func(req *productdto.GetReq) (*productdto.ProductResp, error) {
		return &productdto.ProductResp{ID: "product", ProjectID: "project"}, nil
	}}
	reader := &previewBindingReader{bound: productcontract.BoundTemplateView{Exists: true, InstanceID: "instance", TemplateID: "bound-template"}}
	instances := &fakeDetailInstances{previewResp: &presentationcontract.PreviewInstanceResp{HTML: "<main>bound</main>"}}
	_, err := productservice.PreviewSavedProduct(context.Background(), products, reader, instances, "project", "product")
	if err != nil {
		t.Fatal(err)
	}
	if reader.projectID != products.getReqs[0].ProjectID || reader.productID != products.getReqs[0].ID {
		t.Fatalf("商品和实例必须使用同一作用域：%+v %+v", reader, products.getReqs[0])
	}
	if len(instances.previewReqs) != 1 {
		t.Fatalf("预览调用次数=%d", len(instances.previewReqs))
	}
	req := instances.previewReqs[0]
	if req.InstanceID != reader.bound.InstanceID || req.ProjectID != reader.projectID || req.TemplateID != "" {
		t.Fatalf("已绑定商品应使用实例当前文档：%+v", req)
	}
}

func TestProductPreviewReadFailureDoesNotBuild(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		productErr, boundErr error
	}{
		{name: "商品读取失败", productErr: errors.New("private product read")},
		{name: "绑定读取失败", boundErr: errors.New("private bound read")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			products := &fakeDetailProducts{t: t, get: func(*productdto.GetReq) (*productdto.ProductResp, error) {
				return &productdto.ProductResp{ID: "product", ProjectID: "project"}, tc.productErr
			}}
			reader := &previewBindingReader{err: tc.boundErr}
			instances := &fakeDetailInstances{}
			_, err := productservice.PreviewSavedProduct(context.Background(), products, reader, instances, "project", "product")
			want := tc.productErr
			if want == nil {
				want = tc.boundErr
			}
			if !errors.Is(err, want) || len(instances.previewReqs) != 0 {
				t.Fatalf("读取失败不得构建或吞错：err=%v requests=%+v", err, instances.previewReqs)
			}
		})
	}
}
