package productservice

// 详情模板写动作与预览编排。读取原对象由 product_detail_template_page.go 提供，
// 本文件负责模板文档、首次发布、切换、路径更新与构建预览，不构造页面视图。

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

// DetailTemplateInstancePort 「详情页模板」写动作与预览所需的自动发布能力（消费者侧最窄接口）。
//
// 只列本文件真正用到的四个方法：预览 / 首次发布 / 切换模板重建 / 改 URL。
// HTTP 页面端口另含绑定查询；service 只声明当前用例需要的能力，不反向依赖 HTTP。
type DetailTemplateInstancePort interface {
	PreviewInstance(ctx context.Context, req *presentationcontract.PreviewInstanceReq) (res *presentationcontract.PreviewInstanceResp, err error)
	CreateInstance(ctx context.Context, req *presentationcontract.CreateInstanceReq) (res *presentationcontract.InstanceResp, err error)
	Rebuild(ctx context.Context, req *presentationcontract.RebuildReq) (res *presentationcontract.InstanceResp, err error)
	UpdateURL(ctx context.Context, req *presentationcontract.UpdateURLReq) (res *presentationcontract.InstanceResp, err error)
}

// DetailTemplateDeps 是每个商品处理器独立持有的模板编排依赖。
type DetailTemplateDeps struct {
	Templates contenttemplatecontract.ContentTemplateService // 模板文档组装 / 新建模板
	Instances DetailTemplateInstancePort                     // 发布 / 切换模板 / 改 URL / 预览
	Products  ProductReadPort                                // 发布时取 slug 派生默认路径
	Projects  siteurl.ProjectDetailReader                    // 站点 URL 规则（默认路径的派生依据）
}

// 契约返回空数据属于内部故障，原始原因只进入日志。
var (
	// errDetailTemplateNilGetResult 模板契约 Get 返回 (nil, nil)：下一行就会解引用 res.DraftDocument。
	errDetailTemplateNilGetResult = errors.New("product: 模板契约 Get 返回空结果（nil, nil），取不到模板文档")
	// errDetailTemplateEmptyDoc 复制来源的文档为空（空串 / 纯空白 / JSON null），拒绝建空模板。
	errDetailTemplateEmptyDoc = errors.New("product: 复制来源的模板文档为空，拒绝建没有内容的模板")
)

// DetailTemplateDocument 只读取指定工程内的复制来源或默认商品模板。
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
	// 契约上 Get 成功必给结果，(nil, nil) 属于实现违约。不拦这一条，下一行解引用
	// res.DraftDocument 就是 panic —— 现场只剩一个栈，连「哪一步坏了」都读不出来。
	if res == nil {
		return nil, errDetailTemplateNilGetResult
	}
	return res.DraftDocument, nil
}

// CreateDetailTemplate 新建一套命名模板（复制指定模板或当前默认模板的文档），初始版本 v1。
//
// 顺序与拆分前一致：**先取文档**（DetailTemplateDocument），取不到就不建模板 ——
// 反过来先建空模板再补文档，会在失败时留下一套没有内容的模板。
// 参数校验留在用例边界，HTTP、MCP 和服务直调使用同一规则。
func CreateDetailTemplate(ctx context.Context, deps DetailTemplateDeps, projectID, name, copyFrom string) error {
	projectID, name, copyFrom = strings.TrimSpace(projectID), strings.TrimSpace(name), strings.TrimSpace(copyFrom)
	if name == "" {
		return productenums.Err(productenums.ErrDetailTemplateNameRequired)
	}
	doc, err := DetailTemplateDocument(ctx, deps.Templates, projectID, copyFrom)
	if err != nil {
		return err
	}
	// 防坏数据，不改变非空路径的行为：HTTP 绑定层的 binding:"required" 只在绑定期生效，
	// service 直调（本包测试、MCP 工具、后台脚本）不经它 —— 于是空文档会一路写进库，
	// 表现为「模板建出来了，编辑器打开是一片空白」，而用户当时看到的是成功提示。
	if detailTemplateDocEmpty(doc) {
		return errDetailTemplateEmptyDoc
	}
	_, err = deps.Templates.Create(ctx, &contenttemplatecontract.CreateReq{
		EntityType: productcontract.EntityTypeProduct, Name: name, DraftDocument: doc, ProjectID: projectID,
	})
	return err
}

// detailTemplateDocEmpty 文档是否为空内容（空串 / 纯空白 / JSON null）。
//
// 判据取「解出来的字节里没有内容」，不解析文档结构：文档语义校验是编辑器与
// contenttemplate 契约的职责，这一层只拦「空文档落库」这一种坏数据。
func detailTemplateDocEmpty(doc json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(doc))
	return trimmed == "" || trimmed == "null"
}

// PublishDetailTemplate 首次建立商品发布实例；缺省 URL 由商品与站点规则确定。
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
		// 多工程下这一跳必须带作用域：product 侧 Get 经 resolveProjectID 解析工程，
		// 工程不唯一时它拒绝猜一个、直接回 ErrInvalidParam —— 表现是「发布失败」，
		// 而这个商品的实例本该建得出来（同 CreateInstance 的 ProjectID，见上）。
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

// UpdateDetailTemplateURL 透传迁址业务的 WithRedirect；不产生本次 HTTP 响应的导航。
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

// ApplyDetailTemplate 把商品切换（或确认）到选中的模板并重新发布 —— 产物随模板变化（验收 4）。
//
// 工程作用域同样靠调用方传入（同 UpdateDetailTemplateURL）：RebuildReq.ProjectID 空着时
// presentation 走「工程唯一时取该工程」的兜底，多工程站点会重建别的工程的实例，
// 用户看到「切换成功」而当前站点的产物没有变。
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

// PreviewDetailTemplate 只读构建。未保存、依赖故障、空产物均返回错误事实，不构造提示页面。
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

// ProductPreviewBindingReader 只读取商品当前发布实例事实。
type ProductPreviewBindingReader interface {
	QueryBoundDetailTemplate(context.Context, string, string) (productcontract.BoundTemplateView, error)
}

// PreviewProductDetailTemplate 验证商品及工程归属后预览作者选择的模板。
func PreviewProductDetailTemplate(ctx context.Context, products ProductReadPort, instances DetailTemplateInstancePort,
	projectID, productID, templateID string) (*presentationcontract.PreviewInstanceResp, error) {
	product, err := previewProduct(ctx, products, projectID, productID)
	if err != nil {
		return nil, err
	}
	return PreviewDetailTemplate(ctx, instances, product.ProjectID, product.ID, templateID)
}

// PreviewSavedProduct 优先预览该商品已绑定实例的当前文档；尚未发布时预览默认模板。
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

// ReapplyDetailTemplatePreset 重新套用预设，由发布契约维护重建事务与版本约束。
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

// RollbackDetailTemplateDocument 回滚发布文档；实例、工程和快照事实原样交发布契约判定。
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
