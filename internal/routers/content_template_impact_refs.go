package routers

// content_template_impact_refs.go — 模板引用反查的装配实现（影响面提示与删除保护）。
//
// 端口定义在 contenttemplate 模块（contenttemplatecontract.TemplateImpactPort），
// 实现放在装配层：它要同时读 page 契约（页面草稿文档 settings.structure 的槽位绑定）
// 与 presentation 契约（实例绑定的模板 + 实例文档里的结构绑定）——两个模块的只读面
// 在这里拼装，contenttemplate 模块因此既不认识页面表、也不认识实例表。
//
// 为什么页面侧走 ListDrafts 全站扫描而不是依赖表：
//   · page_dependencies 里的 content_template:{id} 行只覆盖**构建过**的页面，
//     且当前 page 契约没有「按依赖键只读反查」的入口（那是写路径 MarkStaleByDependency），
//     拿一次读操作去改 stale 列是错的；
//   · 页面文档里的 settings.structure 才是绑定本身（构建期就是按它决定用哪套模板），
//     扫描它同时覆盖「还没构建过的页面」——删模板时恰恰要拦住这一类。
// 为什么现在是「文档扫描 ∪ 依赖表反查」（page 契约补上只读入口 FindPagesByDependency 之后）：
//   · 上面两条论证的是「不能用依赖表**替换**扫描」——扫描覆盖还没构建过的页面；
//     不是「依赖表没有用」：两类来源读的是**不同的事实**，扫描读当前草稿声明，
//     依赖表读构建期实际消费过的记录（Artifact Manifest 落到 page_dependencies 的那份）。
//   · 并集才拦得住的两类：只有扫描认得「建站时就绑好、还没构建过」的页面（依赖表里没有它的行）；
//     只有依赖表认得「草稿已改主意 / 文档解析不了，但现行 active 或 staged 产物仍声明着
//     content_template:{id}」的页面 —— 那份产物的字节就是按这套模板渲染出来的。
//   · 去重按 page id 做（add 的 seen 键含模板 id）；扫描先收集，所以两条来源命中同一页面时
//     **保留扫描那条**（它带槽位与草稿路径，对运营更可定位），依赖表只补扫描没给出的页面行。
//   · 只读：走的是 FindPagesByDependency，**不是** MarkStaleByDependency（后者按依赖把命中页面
//     标成 stale）—— 一次读操作不得顺手改库，原注释里的这条论证对新增来源同样成立。
//   · 代价：每个模板 id 各一次按 (dependency_kind, dependency_key) 的索引查询
//     （迁移 071 建了这条索引），与模板数量线性相关；单工程模板数是个位数量级，
//     相对上面那次全站文档扫描可忽略。

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"strings"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"
)

// contentTemplateImpactRefs 引用反查端口的装配实现。
type contentTemplateImpactRefs struct {
	pages    pagecontract.PageService
	presents presentationcontract.PresentationService
}

// ContentTemplateImpactPort 构造模板引用反查端口（装配期交给 contenttemplate service）。
func ContentTemplateImpactPort(pages pagecontract.PageService,
	presentations presentationcontract.PresentationService) contenttemplatecontract.TemplateImpactPort {
	return &contentTemplateImpactRefs{pages: pages, presents: presentations}
}

// wireContentTemplateImpact 把引用反查端口注入 contenttemplate 模块。
//
// 为什么不能省：页面文档里的 settings.structure 绑定写在 JSONB 里，数据库外键管不到。
// 缺这条注入的表现是「删掉一套正被页面当页眉用的模板一路成功」，站点页眉在下一次
// 重建后消失，构建日志里只留一行 Warn —— 所以缺注入必须留下痕迹（Warn），
// 并且页面会把它显示成「引用查不出来」而不是「没有引用」。
func (a *assembly) wireContentTemplateImpact() {
	setter, ok := a.contentTemplateSvc.(interface {
		SetImpactPort(contenttemplatecontract.TemplateImpactPort)
	})
	if !ok {
		logger.Scene("init").Warn("contenttemplate 未提供引用反查注入点（SetImpactPort）：" +
			"模板影响面与删除保护退化为仅查本模块表与实例外键")
		return
	}
	setter.SetImpactPort(ContentTemplateImpactPort(a.pageService, a.presentationSvc))
	a.marks.mark(portContentTemplateImpactPort)
	// 同一批接线里注入「结构模板候选」（主题设置页的「选结构模板」下拉数据源）：
	// 两者都依赖 contenttemplate 契约，分开两处注入只会让「谁负责接哪个端口」变得难查。
	if optSetter, ok := a.projectService.(interface {
		SetStructureTemplateOptionsPort(projectcontract.StructureTemplateOptionsPort)
	}); ok {
		optSetter.SetStructureTemplateOptionsPort(&structureTemplateOptionsPort{templates: a.contentTemplateSvc})
		a.marks.mark(portProjectStructureTemplates)
	} else {
		logger.Scene("init").Warn("project 模块未提供结构模板候选注入点（SetStructureTemplateOptionsPort）：" +
			"主题设置页的「选结构模板」下拉将只有「不绑定」")
	}
}

// ListTemplateReferences 实现 contenttemplatecontract.TemplateImpactPort。
//
// 一次扫描服务整页：页面草稿（全站一次）与实例（本工程一次）各取一遍，
// 在内存里按模板 id 归集，调用方（列表页）不必为每套模板各扫一次。
func (p *contentTemplateImpactRefs) ListTemplateReferences(ctx context.Context, projectID string,
	templateIDs []string) (refs []contenttemplatedto.TemplateReference, unparsable int, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || len(templateIDs) == 0 {
		// 工程作用域是读取前提：空工程 id 一律返回空，**不**退化成「不限工程」。
		return nil, 0, nil
	}
	known := make(map[string]struct{}, len(templateIDs))
	for _, id := range templateIDs {
		if id = strings.TrimSpace(id); id != "" {
			known[id] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	add := func(ref contenttemplatedto.TemplateReference) {
		key := ref.Kind + "|" + ref.TemplateID + "|" + ref.PageID + ref.InstanceID
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
	}

	// 1) 页面草稿文档：settings.structure 的槽位绑定。
	if p.pages != nil {
		drafts, derr := p.pages.ListDrafts(ctx)
		if derr != nil {
			return nil, 0, derr
		}
		for i := range drafts {
			d := &drafts[i]
			if strings.TrimSpace(d.ProjectID) != projectID {
				continue
			}
			title, slotsByTemplate, perr := structureBindingsInDocument(d.DraftDocument)
			if perr != nil {
				unparsable++
				// 解析不了仍然粗判一次（模板 id 是 uuid，误命中概率极低）：
				// 命中即按「引用了、槽位未知」记一条 —— 跳过等于放行一次可能丢绑定的删除。
				for id := range known {
					if bytes.Contains(d.DraftDocument, []byte(id)) {
						add(contenttemplatedto.TemplateReference{
							TemplateID: id, Kind: contenttemplatedto.ReferenceKindPage,
							PageID: d.ID, PagePath: d.DraftPath,
						})
					}
				}
				continue
			}
			for id, slots := range slotsByTemplate {
				if _, ok := known[id]; !ok {
					continue
				}
				add(contenttemplatedto.TemplateReference{
					TemplateID: id, Kind: contenttemplatedto.ReferenceKindPage, Slots: slots,
					PageID: d.ID, PagePath: d.DraftPath, PageTitle: title,
				})
			}
		}
	}

	// 2) 自动发布实例：绑定的模板 + 实例文档里的结构绑定。
	if p.presents != nil {
		list, lerr := p.presents.List(ctx, &presentationdto.ListReq{ProjectID: projectID})
		if lerr != nil {
			return nil, 0, lerr
		}
		for _, inst := range list {
			if inst == nil {
				continue
			}
			if tid := strings.TrimSpace(inst.TemplateID); tid != "" {
				if _, ok := known[tid]; ok {
					add(contenttemplatedto.TemplateReference{
						TemplateID: tid, Kind: contenttemplatedto.ReferenceKindInstance,
						InstanceID: inst.ID, EntityType: inst.EntityType, EntityID: inst.EntityID,
						URLPath: inst.URLPath, RenderMode: inst.RenderMode,
					})
				}
			}
			if len(inst.Document) == 0 {
				continue
			}
			_, slotsByTemplate, perr := structureBindingsInDocument(inst.Document)
			if perr != nil {
				unparsable++
				continue
			}
			for id, slots := range slotsByTemplate {
				if _, ok := known[id]; !ok {
					continue
				}
				add(contenttemplatedto.TemplateReference{
					TemplateID: id, Kind: contenttemplatedto.ReferenceKindInstance, Slots: slots,
					InstanceID: inst.ID, EntityType: inst.EntityType, EntityID: inst.EntityID,
					URLPath: inst.URLPath, RenderMode: inst.RenderMode,
				})
			}
		}
	}

	// 3) page_dependencies 依赖表反查（**只读**）：只覆盖**构建过**的页面，与上面的扫描取并集。
	//
	// 为什么不能省：草稿里已经把绑定改掉 / 文档解析不了、但现行 active 或 staged 产物
	// 仍声明着该模板的那一类页面，只在依赖表里有记录。缺了它，「删掉一套正被线上产物
	// 引用的模板」会一路成功，站点在下次重建后少一套页眉，构建日志里只留一行 Warn。
	//
	// 逐个模板 id 各查一次（不是全表扫）：命中走 page_dependencies 的
	// (dependency_kind, dependency_key) 索引。key 的拼法取自 pipeline.ContentTemplateKey ——
	// 依赖键的格式只有那一处实现，这里手写 "content_template:"+id 迟早与写入侧漂移
	//（漂移的表现是反查永远查不到行，而它不报任何错）。
	if p.pages != nil {
		for id := range known {
			dep := pipeline.ContentTemplateKey(id)
			hit, derr := p.pages.FindPagesByDependency(ctx, projectID, dep.Kind, dep.Key)
			if derr != nil {
				// 失败即失败，不降级成空集合：那会把一次读取失败伪装成「没有引用」，
				// 删除保护据此放行 —— 正是本反查要拦住的场景。
				return nil, 0, derr
			}
			for i := range hit {
				add(contenttemplatedto.TemplateReference{
					TemplateID: id, Kind: contenttemplatedto.ReferenceKindPage,
					PageID: hit[i].ID, PageTitle: hit[i].Title,
				})
			}
		}
	}

	// 确定性输出：同一份数据渲染出来的行序不随 map 迭代变化（列表页会按它渲染）。
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].TemplateID != refs[j].TemplateID {
			return refs[i].TemplateID < refs[j].TemplateID
		}
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		return refs[i].PageID+refs[i].InstanceID < refs[j].PageID+refs[j].InstanceID
	})
	return refs, unparsable, nil
}

// structureBindingsInDocument 从一份 Page Document 里取出「模板 id → 命中的槽位」与 SEO 标题。
//
// 用 builder.StructureBindings 解析而不是手写 map 取值：头部 / 页脚两条旧通道
// （headerTemplateId / footerTemplateId）与 slotTemplates 的合并规则只有那一处实现，
// 手抄一份必然与构建期漂移（漏掉旧通道 = 漏掉一半绑定，而且只在删除保护上表现为放行）。
func structureBindingsInDocument(doc json.RawMessage) (title string,
	slotsByTemplate map[string][]string, err error) {
	if len(doc) == 0 {
		return "", nil, nil
	}
	var parsed struct {
		Settings struct {
			Structure builder.StructureBindings `json:"structure"`
			SEO       struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	if jerr := json.Unmarshal(doc, &parsed); jerr != nil {
		return "", nil, jerr
	}
	for slot, id := range parsed.Settings.Structure.TemplateBindings() {
		if slotsByTemplate == nil {
			slotsByTemplate = map[string][]string{}
		}
		slotsByTemplate[id] = append(slotsByTemplate[id], slot)
	}
	for id := range slotsByTemplate {
		sort.Strings(slotsByTemplate[id])
	}
	return strings.TrimSpace(parsed.Settings.SEO.Title), slotsByTemplate, nil
}

// structureTemplateOptionsPort 主题设置页「选结构模板」下拉的候选来源。
//
// 走 contenttemplate 契约的只读 List（按显式工程），只保留结构模板类型：
// 下拉里出现内容实体模板（product / article）是错的 —— 那类模板的绑定入口在
// 商品 / 文章侧，混进来只会让人以为可以把它当页眉用。
type structureTemplateOptionsPort struct {
	templates contenttemplatecontract.ContentTemplateService
}

// ListStructureTemplateOptions 实现 projectcontract.StructureTemplateOptionsPort。
func (p *structureTemplateOptionsPort) ListStructureTemplateOptions(ctx context.Context, projectID string) (
	opts []projectcontract.StructureTemplateOption, err error) {
	if p == nil || p.templates == nil || strings.TrimSpace(projectID) == "" {
		return nil, nil
	}
	list, lerr := p.templates.List(ctx, &contenttemplatedto.ListReq{ProjectID: projectID})
	if lerr != nil {
		return nil, lerr
	}
	for _, t := range list {
		if t == nil || !contenttemplatecontract.IsStructureTemplateType(t.EntityType) {
			continue
		}
		opts = append(opts, projectcontract.StructureTemplateOption{
			ID: t.ID, Name: t.Name, EntityType: t.EntityType, IsDefault: t.IsDefault,
		})
	}
	return opts, nil
}

// 结构类型判定统一走 contenttemplatecontract.IsStructureTemplateType（白名单的唯一真源）：
// 本文件曾自己抄一份 switch，两份字面量一旦漂移，新增结构类型只会在这里静默漏掉
//（表现是「主题里的页眉下拉少了新类型」而不是编译错误）。
