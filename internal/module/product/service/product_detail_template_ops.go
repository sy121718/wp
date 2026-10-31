package productservice

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/siteurl"
)

type DetailTemplateInstancePort interface {
	PreviewInstance(ctx context.Context, req *presentationcontract.PreviewInstanceReq) (res *presentationcontract.PreviewInstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationcontract.CreateInstanceReq) (res *presentationcontract.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationcontract.RebuildReq) (res *presentationcontract.InstanceResp, err error)
	UpdateURL(ctx context.Context, req *presentationcontract.UpdateURLReq) (res *presentationcontract.InstanceResp, err error)
}

type DetailTemplateDeps struct {
	Templates contenttemplatecontract.ContentTemplateService
	Instances DetailTemplateInstancePort
	Products  ProductReadPort
	Projects  siteurl.ProjectDetailReader
}

var (
	errDetailTemplateNilGetResult = errors.New("product: 模板契约 Get 返回空结果（nil, nil），取不到模板文档")

	errDetailTemplateEmptyDoc = errors.New("product: 复制来源的模板文档为空，拒绝建没有内容的模板")
)

func DetailTemplateDocument(ctx context.Context, templates contenttemplatecontract.ContentTemplateService,
	projectID, templateID string) (doc json.RawMessage, err error) {
	if templates == nil {
		return nil, productenums.Err(productenums.ErrDetailTemplateUnavailable)
	}
	projectID = strings.TrimSpace(projectID)
	id := strings.TrimSpace(templateID)
	if id == "" {
		tpl, rerr := templates.ResolveTemplateScoped(ctx, projectID, productcontract.EntityTypeProduct)
		if rerr != nil {
			return nil, rerr
		}
		if tpl == nil {
			return nil, errDetailTemplateNilGetResult
		}
		id = tpl.TemplateID
	}
	res, gerr := templates.GetScoped(ctx, projectID, id)
	if gerr != nil {
		return nil, gerr
	}

	if res == nil {
		return nil, errDetailTemplateNilGetResult
	}
	return res.DraftDocument, nil
}

func CreateDetailTemplate(ctx context.Context, deps DetailTemplateDeps, projectID, name, copyFrom string) error {
	projectID, name, copyFrom = strings.TrimSpace(projectID), strings.TrimSpace(name), strings.TrimSpace(copyFrom)
	if name == "" {
		return productenums.Err(productenums.ErrDetailTemplateNameRequired)
	}
	doc, err := DetailTemplateDocument(ctx, deps.Templates, projectID, copyFrom)
	if err != nil {
		return err
	}

	if detailTemplateDocEmpty(doc) {
		return errDetailTemplateEmptyDoc
	}
	_, err = deps.Templates.Create(ctx, &contenttemplatecontract.CreateReq{
		EntityType: productcontract.EntityTypeProduct, Name: name, DraftDocument: doc, ProjectID: projectID,
	})
	return err
}

func detailTemplateDocEmpty(doc json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(doc))
	return trimmed == "" || trimmed == "null"
}

func PublishDetailTemplate(ctx context.Context, deps DetailTemplateDeps, projectID, productID, templateID, urlPath string) error {
	projectID, productID = strings.TrimSpace(projectID), strings.TrimSpace(productID)
	templateID, urlPath = strings.TrimSpace(templateID), strings.TrimSpace(urlPath)
	if productID == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	if deps.Instances == nil || (urlPath == "" && deps.Products == nil) {
		return productenums.Err(productenums.ErrDetailTemplateUnavailable)
	}
	req := &presentationcontract.CreateInstanceReq{
		ProjectID: projectID, EntityType: productcontract.EntityTypeProduct, EntityID: productID,
		TemplateID: templateID, URLPath: urlPath,
	}
	if req.URLPath == "" {

		product, err := deps.Products.Get(ctx, &productdto.GetReq{ID: productID, ProjectID: projectID})
		if err != nil {
			return err
		}
		if product == nil {
			return productenums.Err(productenums.ErrNotFound)
		}
		req.URLPath = siteurl.DetailPathForProject(ctx, deps.Projects, projectID,
			siteurl.KindProduct, product.Slug, productID)
	}
	_, err := deps.Instances.CreateInstance(ctx, req)
	return err
}

func UpdateDetailTemplateURL(ctx context.Context, instances DetailTemplateInstancePort,
	projectID, productID, newPath string, withRedirect bool) error {
	projectID, productID, newPath = strings.TrimSpace(projectID), strings.TrimSpace(productID), strings.TrimSpace(newPath)
	if productID == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	if newPath == "" {
		return productenums.Err(productenums.ErrDetailTemplatePathRequired)
	}
	if instances == nil {
		return productenums.Err(productenums.ErrDetailTemplateUnavailable)
	}
	_, err := instances.UpdateURL(ctx, &presentationcontract.UpdateURLReq{
		EntityType:   productcontract.EntityTypeProduct,
		EntityID:     productID,
		ProjectID:    projectID,
		NewPath:      newPath,
		WithRedirect: withRedirect,
	})
	return err
}

func ApplyDetailTemplate(ctx context.Context, instances DetailTemplateInstancePort,
	projectID, productID, templateID string) error {
	projectID, productID, templateID = strings.TrimSpace(projectID), strings.TrimSpace(productID), strings.TrimSpace(templateID)
	if productID == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	if instances == nil {
		return productenums.Err(productenums.ErrDetailTemplateUnavailable)
	}
	_, err := instances.Rebuild(ctx, &presentationcontract.RebuildReq{
		EntityID: productID, ProjectID: projectID, TemplateID: templateID,
	})
	return err
}

func PreviewDetailTemplate(ctx context.Context, instances DetailTemplateInstancePort,
	projectID, entityID, templateID string) (*presentationcontract.PreviewInstanceResp, error) {
	projectID, entityID, templateID = strings.TrimSpace(projectID), strings.TrimSpace(entityID), strings.TrimSpace(templateID)
	if entityID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	if instances == nil {
		return nil, productenums.Err(productenums.ErrDetailTemplateUnavailable)
	}
	res, err := instances.PreviewInstance(ctx, &presentationcontract.PreviewInstanceReq{
		ProjectID: projectID, EntityType: productcontract.EntityTypeProduct,
		EntityID: entityID, TemplateID: templateID,
	})
	return previewResult(res, err)
}

type ProductPreviewBindingReader interface {
	QueryBoundDetailTemplate(context.Context, string, string) (productcontract.BoundTemplateView, error)
}

func PreviewProductDetailTemplate(ctx context.Context, products ProductReadPort, instances DetailTemplateInstancePort,
	projectID, productID, templateID string) (*presentationcontract.PreviewInstanceResp, error) {
	product, err := previewProduct(ctx, products, projectID, productID)
	if err != nil {
		return nil, err
	}
	return PreviewDetailTemplate(ctx, instances, product.ProjectID, product.ID, templateID)
}

func PreviewSavedProduct(ctx context.Context, products ProductReadPort, reader ProductPreviewBindingReader, instances DetailTemplateInstancePort,
	projectID, productID string) (*presentationcontract.PreviewInstanceResp, error) {
	product, err := previewProduct(ctx, products, projectID, productID)
	if err != nil {
		return nil, err
	}
	if instances == nil || reader == nil {
		return nil, productenums.Err(productenums.ErrDetailTemplateUnavailable)
	}
	bound, err := reader.QueryBoundDetailTemplate(ctx, product.ProjectID, product.ID)
	if err != nil {
		return nil, err
	}
	if !bound.Exists {
		return PreviewDetailTemplate(ctx, instances, product.ProjectID, product.ID, "")
	}
	res, err := instances.PreviewInstance(ctx, &presentationcontract.PreviewInstanceReq{ProjectID: product.ProjectID, InstanceID: bound.InstanceID})
	return previewResult(res, err)
}

func previewProduct(ctx context.Context, products ProductReadPort, projectID, productID string) (*productdto.ProductResp, error) {
	projectID, productID = strings.TrimSpace(projectID), strings.TrimSpace(productID)
	if productID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	if products == nil {
		return nil, productenums.Err(productenums.ErrDetailTemplateUnavailable)
	}
	product, err := products.Get(ctx, &productdto.GetReq{ProjectID: projectID, ID: productID})
	if err != nil {
		return nil, err
	}
	if product == nil || (projectID != "" && product.ProjectID != projectID) {
		return nil, productenums.Err(productenums.ErrNotFound)
	}
	return product, nil
}

func previewResult(res *presentationcontract.PreviewInstanceResp, err error) (*presentationcontract.PreviewInstanceResp, error) {
	if err != nil {
		return nil, err
	}
	if res == nil || strings.TrimSpace(res.HTML) == "" {
		return nil, productenums.Err(productenums.ErrDetailTemplatePreviewEmpty)
	}
	return res, nil
}

func ReapplyDetailTemplatePreset(ctx context.Context, port presentationcontract.DetailTemplateModePort, req *productdto.DetailTemplateModeReapplyReq) error {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	if port == nil {
		return productenums.Err(productenums.ErrDetailTemplateUnavailable)
	}
	_, err := port.ReapplyPreset(ctx, &presentationcontract.ReapplyPresetReq{
		InstanceID: strings.TrimSpace(req.InstanceID), ProjectID: strings.TrimSpace(req.ProjectID), TemplateID: strings.TrimSpace(req.TemplateID),
	})
	return err
}

func RollbackDetailTemplateDocument(ctx context.Context, port presentationcontract.DetailTemplateModePort, req *productdto.DetailTemplateModeRollbackReq) error {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.SnapshotID) == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}
	if port == nil {
		return productenums.Err(productenums.ErrDetailTemplateUnavailable)
	}
	_, err := port.RollbackDocument(ctx, &presentationcontract.RollbackDocumentReq{
		InstanceID: strings.TrimSpace(req.InstanceID), ProjectID: strings.TrimSpace(req.ProjectID), SnapshotID: strings.TrimSpace(req.SnapshotID),
	})
	return err
}
