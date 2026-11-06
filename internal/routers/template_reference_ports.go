package routers

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"gorm.io/gorm"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
)

// templateReferenceAggregate 把页面 / 实例 / 主题三处的事务内反查合成
// contenttemplate 删除保护需要的单一事实集合。
//
// 三个读口都必须使用调用方传入的 tx：删除方已在同一事务里取了工程内容写锁，
// 另开事务会读到锁外快照，「持锁复核」就不成立了。
type templateReferenceAggregate struct {
	pages    pagecontract.TemplateRefTxReader
	presents presentationcontract.TemplateRefTxReader
	themes   projectcontract.TemplateRefTxReader
}

var _ contenttemplatecontract.TemplateReferenceTxReader = (*templateReferenceAggregate)(nil)

func (a *templateReferenceAggregate) ListTemplateReferencesTx(ctx context.Context, tx *gorm.DB,
	projectID string, query *contenttemplatecontract.TemplateRefQuery) ([]contenttemplatecontract.TemplateReferenceRow, error) {
	if query == nil {
		return nil, nil
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, contenttemplateenums.Err(contenttemplateenums.ErrProjectRequired)
	}
	var (
		out []contenttemplatecontract.TemplateReferenceRow
		err error
	)
	if a.pages != nil {
		var refs []pagecontract.TemplateRef
		if refs, err = a.pages.ListPageTemplateRefsTx(ctx, tx, projectID, query.TemplateIDs); err != nil {
			return nil, err
		}
		for _, ref := range refs {
			label := strings.TrimSpace(ref.Title)
			if label == "" {
				label = strings.TrimSpace(ref.Path)
			}
			out = append(out, contenttemplatecontract.TemplateReferenceRow{
				TemplateID: ref.TemplateID, Kind: contenttemplatecontract.TemplateReferenceKindPage,
				EntityID: ref.PageID, Label: label, Detail: templateSlotsDetail(ref.Slots),
			})
		}
	}
	if a.presents != nil {
		var refs []presentationcontract.TemplateRef
		if refs, err = a.presents.ListInstanceTemplateRefsTx(ctx, tx, projectID, query.TemplateIDs, query.VersionIDs); err != nil {
			return nil, err
		}
		for _, ref := range refs {
			label := strings.TrimSpace(ref.URLPath)
			if label == "" {
				label = strings.TrimSpace(ref.EntityType) + " " + strings.TrimSpace(ref.EntityID)
			}
			out = append(out, contenttemplatecontract.TemplateReferenceRow{
				TemplateID: ref.TemplateID, Kind: contenttemplatecontract.TemplateReferenceKindInstance,
				EntityID: ref.InstanceID, Label: label, Detail: templateSlotsDetail(ref.Slots),
			})
		}
	}
	if a.themes != nil {
		var refs []projectcontract.TemplateRef
		if refs, err = a.themes.ListThemeTemplateRefsTx(ctx, tx, projectID, query.TemplateIDs); err != nil {
			return nil, err
		}
		for _, ref := range refs {
			out = append(out, contenttemplatecontract.TemplateReferenceRow{
				TemplateID: ref.TemplateID, Kind: contenttemplatecontract.TemplateReferenceKindTheme,
				EntityID: ref.ThemeID, Label: strings.TrimSpace(ref.ThemeName), Detail: templateSlotsDetail(ref.Slots),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TemplateID != out[j].TemplateID {
			return out[i].TemplateID < out[j].TemplateID
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].EntityID < out[j].EntityID
	})
	return out, nil
}

// templateSlotsDetail 把槽位列表拼成可定位明细；没有槽位时返回空串。
func templateSlotsDetail(slots []string) string {
	if len(slots) == 0 {
		return ""
	}
	sorted := append([]string(nil), slots...)
	sort.Strings(sorted)
	return strings.Join(sorted, " / ")
}

// wireTemplateReferencePorts 装配模板删除保护与写入方存在性复核。
//
// 删除方（contenttemplate）拿到引用聚合；写入方（page / project）拿到存在性复核。
// 两者共用工程内容写锁，缺任一端都会让「校验通过、提交时已被删除」成为可能，
// 因此这里按必需端口处理：任一注不进去就报告，不静默降级。
func (a *assembly) wireTemplateReferencePorts() {
	refSetter, ok := a.contentTemplateSvc.(interface {
		SetReferencePort(contenttemplatecontract.TemplateReferenceTxReader)
	})
	if !ok {
		logger.Scene("init").Error(
			facing.WithStatus(edgeErr("contenttemplate.SetReferencePort"), http.StatusInternalServerError),
			"内容模板未提供引用反查注入点：模板删除将 fail-closed 拒绝")
	} else {
		aggregate := &templateReferenceAggregate{}
		if reader, ok := a.pageService.(pagecontract.TemplateRefTxReader); ok {
			aggregate.pages = reader
		} else {
			logger.Scene("init").Error(facing.WithStatus(edgeErr("page.TemplateRefTxReader"), http.StatusInternalServerError),
				"页面模块未实现事务内模板引用反查：删除保护会漏掉页面引用")
		}
		if reader, ok := a.presentationSvc.(presentationcontract.TemplateRefTxReader); ok {
			aggregate.presents = reader
		} else {
			logger.Scene("init").Error(facing.WithStatus(edgeErr("presentation.TemplateRefTxReader"), http.StatusInternalServerError),
				"自动发布模块未实现事务内模板引用反查：删除保护会漏掉实例引用")
		}
		if reader, ok := a.projectService.(projectcontract.TemplateRefTxReader); ok {
			aggregate.themes = reader
		} else {
			logger.Scene("init").Error(facing.WithStatus(edgeErr("project.TemplateRefTxReader"), http.StatusInternalServerError),
				"项目模块未实现事务内模板引用反查：删除保护会漏掉主题引用")
		}
		refSetter.SetReferencePort(aggregate)
		a.marks.mark(portContentTemplateReferencePort)
	}

	existence, ok := a.contentTemplateSvc.(contenttemplatecontract.TemplateExistenceTxReader)
	if !ok {
		// 不登记 marks：它是必需端口，缺失要让启动自检明确报出来，而不是伪装成已接线。
		logger.Scene("init").Error(
			facing.WithStatus(edgeErr("contenttemplate.TemplateExistenceTxReader"), http.StatusInternalServerError),
			"内容模板未实现事务内模板存在性复核端口")
		return
	}

	if setter, ok := a.pageService.(interface {
		SetTemplateExistencePort(contenttemplatecontract.TemplateExistenceTxReader)
	}); ok {
		setter.SetTemplateExistencePort(existence)
		a.marks.mark(portPageTemplateExistence)
	} else {
		logger.Scene("init").Error(
			facing.WithStatus(edgeErr("page.SetTemplateExistencePort"), http.StatusInternalServerError),
			"页面模块未提供模板存在性复核注入点")
	}
	if setter, ok := a.projectService.(interface {
		SetTemplateExistencePort(contenttemplatecontract.TemplateExistenceTxReader)
	}); ok {
		setter.SetTemplateExistencePort(existence)
		a.marks.mark(portProjectTemplateExistence)
	} else {
		logger.Scene("init").Error(
			facing.WithStatus(edgeErr("project.SetTemplateExistencePort"), http.StatusInternalServerError),
			"项目模块未提供模板存在性复核注入点")
	}
}

// edgeErr 是装配期缺口的错误事实；它只在启动日志里出现，不作为对外文案。
func edgeErr(key string) error { return facing.New(key) }
