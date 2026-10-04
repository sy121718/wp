package producthttp

// product_detail_mode_page.go — 商品详情页的双轨写动作（迁移 282，docs/04-C-instance-override.md）。
//
// 三个动作都只改「这一个商品」的呈现，不碰共享模板；原生表单 POST + 302 回详情页，
// 与商品页其它写动作同一形态（form 里带 csrf_token 隐藏域，路由挂 pages 组）：
//
//	POST /admin/products/reapply-preset     放弃独立文档，回到跟随模板（可反悔的另一半）
//	POST /admin/products/rollback-document  取历史快照的文档重发（重新编译，数据取最新）
//	POST /admin/products/rollback-artifact  产物指针回滚（秒级，不重新编译）
//
// 权限复用「商品更新」权限点（与详情页其它写动作一致）：路由上显式指定
// CasbinMiddlewareForPath("/api/product/update")，因此不需要新增权限点 seed。
//
// 成功回执不写 ?done= 文案：页面上「模式徽标」本身就是结果（跟随模板 ↔ 独立文档），
// 而回执文案必须过白名单词条（productFacingNotice）——为一句提示新增词条与 seed
// 不划算；失败仍走 ?err=（白名单 + 归口文案）。

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
)

// errModeUnavailable 双轨能力未装配（装配缺陷）：走归口文案，不直出原文。
var errModeUnavailable = errors.New("详情页双轨能力未装配")

// modeWriteTarget 解析双轨写动作的目标（工程 / 商品 / 发布实例）。
//
// 实例定位一律经 GetByEntity（工程作用域内），失败按 ErrNotFound 回带 —— 与
// 详情页其它写动作的失败口径一致（用户看到的是「商品不存在」，而不是内部错误）。
func (h *productPageHandle) modeWriteTarget(c *gin.Context) (
	projectID, productID string, inst *presentationcontract.InstanceResp, err error) {
	if h.modePort == nil || h.templates == nil || h.instances == nil {
		return "", "", nil, errModeUnavailable
	}
	projectID = strings.TrimSpace(c.PostForm("projectId"))
	productID = strings.TrimSpace(c.PostForm("productId"))
	if projectID == "" || productID == "" {
		return projectID, productID, nil, errors.New(presentationenums.ErrInvalidParam)
	}
	inst, ierr := h.instances.GetByEntity(c.Request.Context(), &presentationcontract.GetByEntityReq{
		EntityType: productEntityType, EntityID: productID, ProjectID: projectID,
	})
	if ierr != nil || inst == nil {
		return projectID, productID, nil, errors.New(presentationenums.ErrNotFound)
	}
	return projectID, productID, inst, nil
}

// modeRedirect 统一的落点：成功回详情页，失败带白名单文案。
func modeRedirect(c *gin.Context, projectID, productID string, err error) {
	if err != nil {
		c.Redirect(http.StatusFound, productEditLocation(projectID, productID, productErrText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, productEditLocation(projectID, productID, ""))
}

// ProductsReapplyPreset 重新套用预设：放弃该商品独立文档，回到跟随模板。
func (h *productPageHandle) ProductsReapplyPreset(c *gin.Context) {
	projectID, productID, inst, err := h.modeWriteTarget(c)
	if err != nil {
		modeRedirect(c, projectID, productID, err)
		return
	}
	_, err = h.modePort.ReapplyPreset(c.Request.Context(), &presentationcontract.ReapplyPresetReq{
		InstanceID: inst.ID, ProjectID: projectID,
		// 可选：同时换一套模板（换底稿）。留空 = 沿用当前绑定。
		TemplateID: strings.TrimSpace(c.PostForm("templateId")),
	})
	modeRedirect(c, projectID, productID, err)
}

// ProductsRollbackDocument 快照级文档回滚：取历史快照的文档重发。
func (h *productPageHandle) ProductsRollbackDocument(c *gin.Context) {
	projectID, productID, inst, err := h.modeWriteTarget(c)
	if err != nil {
		modeRedirect(c, projectID, productID, err)
		return
	}
	snapshotID := strings.TrimSpace(c.PostForm("snapshotId"))
	if snapshotID == "" {
		modeRedirect(c, projectID, productID, errors.New(presentationenums.ErrInvalidParam))
		return
	}
	_, err = h.modePort.RollbackDocument(c.Request.Context(), &presentationcontract.RollbackDocumentReq{
		InstanceID: inst.ID, ProjectID: projectID, SnapshotID: snapshotID,
	})
	modeRedirect(c, projectID, productID, err)
}

// ProductsRollbackArtifact 产物指针回滚（秒级，不重新编译）。
func (h *productPageHandle) ProductsRollbackArtifact(c *gin.Context) {
	projectID, productID, inst, err := h.modeWriteTarget(c)
	if err != nil {
		modeRedirect(c, projectID, productID, err)
		return
	}
	targetHash := strings.TrimSpace(c.PostForm("targetHash"))
	if targetHash == "" {
		modeRedirect(c, projectID, productID, errors.New(presentationenums.ErrInvalidParam))
		return
	}
	_, err = h.modePort.RollbackArtifact(c.Request.Context(), &presentationcontract.RollbackArtifactReq{
		InstanceID: inst.ID, ProjectID: projectID, TargetHash: targetHash,
	})
	modeRedirect(c, projectID, productID, err)
}
