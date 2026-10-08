package projectservice

// Package projectservice 实现 project 模块业务用例。

// 读的是 projects.settings 这一列（与 GA4 / GSC / Head-Body 同源），不新增存储。
// 形状校验走 projectdto.NormalizeShippingPolicy —— 与后台保存**同一份判据**：
// 两边各写一份的结果是「后台存进去了、结算时按不收运费处理」，不报错、不记日志。

// 这个值只读、不写：写入路径是设置页的整页保存（SaveSiteSettings → saveLangURLMode
// 落进 projects.settings.langURLMode）。此处刻意不做「写入后热更新某个进程级变量」——
// 那正是本批消灭的缺陷（见契约 ProjectService.SiteLangURLMode 的注释）。

// 数据源是注入的 StructureTemplateOptionsPort（装配层用 contenttemplate 契约的只读 List
// 实现）：project 模块不认识 content_templates 表，也不 import contenttemplate 的
// service/model —— 端口留在消费者侧，与本模块的 LocaleRetirePort 同一形状。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/project/dto"
	"go_wp/internal/module/project/enums"
	"go_wp/internal/module/project/model"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

var _ projectcontract.ProjectService = (*Service)(nil)

// Service 站点工程业务服务。
type Service struct {
	model *projectmodel.Model
	// retire 语言下线端口（审计 I18N-017，装配期注入）。
	//
	// **必须注入**（审计 CQ-019 判为 required-port）：为空时禁用语言只记日志、不清路由 ——
	// 运营以为某个语言已下线，其实它的站点仍在线上可访问，且全程没有任何报错。
	// 实现方 page.Service 有编译期断言（var _ projectcontract.LocaleRetirePort），
	// 装配方 routes.go 在断言失败时直接 panic，故生产路径恒非 nil；
	// 本字段的 nil 分支只服务「不经装配、直接构造 Service」的纯单测。
	retire projectcontract.LocaleRetirePort
	// structureTemplateOpts 结构模板候选端口（主题设置页的「选结构模板」下拉，装配期注入）。
	//
	// 可空（非 required-port）：未注入时下拉只有「不绑定」一项、并留一行 Warn，
	// 而不是把「保存主题设置」整件事挡住 —— 这是配置面变窄，不是数据风险。
	structureTemplateOpts projectcontract.StructureTemplateOptionsPort
}

// SetStructureTemplateOptionsPort 注入结构模板候选端口（装配期调用）。
func (s *Service) SetStructureTemplateOptionsPort(p projectcontract.StructureTemplateOptionsPort) {
	s.structureTemplateOpts = p
}

// SetLocaleRetirePort 注入语言下线端口（装配期调用；**必须注入**，理由见字段注释）。
//
// 装配自检：routes.go 在提供方未实现该契约时 panic（审计 CQ-019），
// 清单登记在 internal/routers/wiring.go 的 wiringManifest。
func (s *Service) SetLocaleRetirePort(port projectcontract.LocaleRetirePort) { s.retire = port }

// NewService 创建站点工程服务。
func NewService(model *projectmodel.Model) *Service {
	return &Service{model: model}
}

// project 业务错误哨兵（errors.Is 判型；文案统一取 projectenums，不硬编码）。
var (
	ErrProjectNotFound = errors.New(projectenums.ErrProjectNotFound)
	ErrInvalidName     = errors.New(projectenums.ErrInvalidName)
	ErrInvalidSettings = errors.New(projectenums.ErrInvalidSettings)
	ErrInvalidParam    = errors.New(projectenums.ErrInvalidParam)
)

// Create 创建站点工程与初始 SiteSettings。
func (s *Service) Create(ctx context.Context, req *projectdto.CreateReq) (res *projectdto.ProjectResp, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		return nil, ErrInvalidName
	}
	settings, err := normalizeSettings(req.Settings)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := &projectmodel.ProjectEntity{
		ID: uuid.NewString(), Name: name, Settings: settings, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.model.Create(ctx, e); err != nil {
		return nil, err
	}
	// 建站即有主题：继承链「主题 → 页面 → 组件」要求先有主题（用后台风格的默认主题兜底）。
	// 创建失败不阻塞建站 —— 工程本身可用，主题可在后台补建（启动时也有幂等补齐）。
	if _, terr := s.ensureDefaultTheme(ctx, e.ID); terr != nil {
		logger.Scene("project").With("project", e.ID).Error(terr, "创建默认主题失败")
	}
	return toResp(e), nil
}

// Detail 查询站点工程详情。
func (s *Service) Detail(ctx context.Context, req *projectdto.DetailReq) (res *projectdto.ProjectResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrProjectNotFound
	}
	e, err := s.model.GetByID(ctx, req.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Update 更新站点工程名称与 SiteSettings。
func (s *Service) Update(ctx context.Context, req *projectdto.UpdateReq) (res *projectdto.ProjectResp, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, ErrProjectNotFound
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > 200 {
		return nil, ErrInvalidName
	}
	settings, err := normalizeSettings(req.Settings)
	if err != nil {
		return nil, err
	}
	if _, err = s.model.GetByID(ctx, req.ID); errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrProjectNotFound
	} else if err != nil {
		return nil, err
	}
	if err = s.model.Update(ctx, req.ID, name, settings, time.Now().UTC()); err != nil {
		return nil, err
	}
	e, err := s.model.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Exists 判断站点工程是否存在，供其他模块通过契约校验归属。
func (s *Service) Exists(ctx context.Context, id string) (exists bool, err error) {
	if strings.TrimSpace(id) == "" {
		return false, nil
	}
	if _, err = s.model.GetByID(ctx, id); errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// normalizeSettings 校验设置必须为 JSON 对象；空值规范为 {}。
func normalizeSettings(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, ErrInvalidSettings
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return nil, ErrInvalidSettings
	}
	return normalized, nil
}

func toResp(e *projectmodel.ProjectEntity) *projectdto.ProjectResp {
	return &projectdto.ProjectResp{
		ID: e.ID, Name: e.Name, Settings: e.Settings, CreatedAt: utils.NewJSONTime(e.CreatedAt), UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
}

// List 列出全部站点工程。
func (s *Service) List(ctx context.Context) (res []projectdto.ProjectResp, err error) {
	entities, err := s.model.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	res = make([]projectdto.ProjectResp, 0, len(entities))
	for i := range entities {
		res = append(res, projectdto.ProjectResp{
			ID: entities[i].ID, Name: entities[i].Name, Settings: entities[i].Settings,
			CreatedAt: utils.NewJSONTime(entities[i].CreatedAt), UpdatedAt: utils.NewJSONTime(entities[i].UpdatedAt),
		})
	}
	return res, nil
}

// ListRetentionPolicies 列出启用访问明细自动清理的工程（保留期 > 0）。
//
// analytics 的保留期清理（PurgeExpiredViews）经契约消费这份清单：保留期这一列长在
// projects 表上，读它就该留在本模块，而不是让 analytics 的 model 越过模块边界查表。
//
// 只返回两个字段：调用方要的是「清哪个工程、保留多少天」，其余工程字段与它无关。
func (s *Service) ListRetentionPolicies(ctx context.Context) (res []projectdto.RetentionPolicyResp, err error) {
	entities, err := s.model.ListRetentionPolicies(ctx)
	if err != nil {
		return nil, err
	}
	res = make([]projectdto.RetentionPolicyResp, 0, len(entities))
	for i := range entities {
		res = append(res, projectdto.RetentionPolicyResp{
			ProjectID:     entities[i].ID,
			RetentionDays: entities[i].AnalyticsRetentionDays,
		})
	}
	return res, nil
}

// 编译期断言：本 service 实现站点运费规则读取端口（装配层据此注入给 cart）。
var _ projectcontract.ShippingPolicyReader = (*Service)(nil)

// ShippingPolicyOf 读该工程当前的站点运费规则（分）。
//
// 两种「读不出规则」的情形都在这里就地收敛成零值（= 不收运费），而不是把它们
// 变成 error 让调用方各自解释：
//
//	工程不存在      —— 结算链路本身会在建单时被订单域拒掉（工程不存在则商品也取不到），
//	                   运费的正确答案是「不知道」，不该在这里制造第二个失败点；
//	存储值非法      —— 只有绕过后台表单写入才可能出现（表单保存时已拒绝非法值），
//	                   按 0 走 + 一条 Error 日志：运营能在日志里找到那个工程 id，
//	                   而结算不会因为一个坏配置整店停摆。
//
// error 只留给基础设施故障（读库失败）：那是调用方必须自己决定失效方向的情形
// （cart 侧按「不收运费」降级，见 cart 模块的 cart.go）。
func (s *Service) ShippingPolicyOf(ctx context.Context, projectID string) (policy projectdto.ShippingPolicy, err error) {
	id := strings.TrimSpace(projectID)
	if id == "" {
		return projectdto.ShippingPolicy{}, nil
	}
	e, gerr := s.model.GetByID(ctx, id)
	if errors.Is(gerr, gorm.ErrRecordNotFound) {
		logger.Scene("project").With("project", id).
			Warn("读站点运费规则时工程不存在，本次结算按不收运费处理")
		return projectdto.ShippingPolicy{}, nil
	}
	if gerr != nil {
		return projectdto.ShippingPolicy{}, gerr
	}
	fields := projectdto.ParseSiteSettings(e.Settings)
	policy, nerr := projectdto.NormalizeShippingPolicy(fields.ShippingBaseFee, fields.ShippingFreeThreshold)
	if nerr != nil {
		// 原文进日志（带工程 id 便于定位到具体哪一行配置），对外按零值继续。
		logger.Scene("project").With("project", id).Error(nerr,
			"站点运费配置非法（负数或超上限），本次结算按不收运费处理；请到站点设置页改正")
		return projectdto.ShippingPolicy{}, nil
	}
	return policy, nil
}

// SiteLangURLMode 返回该工程配置的站点语言 URL 方案原文（未配置返回空串）。
//
// 工程不存在按「未配置」处理（返回空串、不报错）：调用方据此回退全局默认方案，
// 与「工程刚被删、构建还在跑」这种时序一致 —— 报错会让一次构建因为工程表的一行
// 消失而失败，而那份构建本来也产不出有用的东西。
func (s *Service) SiteLangURLMode(ctx context.Context, projectID string) (raw string, err error) {
	id := strings.TrimSpace(projectID)
	if id == "" {
		return "", nil
	}
	e, err := s.model.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	if e == nil {
		return "", nil
	}
	return strings.TrimSpace(projectdto.ParseSiteSettings(e.Settings).LangURLMode), nil
}

// StructureTemplateOptions 列出本工程可绑定的结构模板（页眉 / 页脚）。
//
// 未注入端口时返回空列表：调用方（主题设置页）会渲染成只有「不绑定」一项的下拉。
// 这里不返回错误 —— 下拉缺候选是配置面变窄，把它变成一次保存失败是更坏的选择；
// 能力缺失由 Warn 日志与页面数据（候选为空）共同表达。
func (s *Service) StructureTemplateOptions(ctx context.Context, projectID string) (
	opts []projectcontract.StructureTemplateOption, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, nil
	}
	if s.structureTemplateOpts == nil {
		logger.Scene("theme").Warn("结构模板候选端口未装配：主题设置页的「选结构模板」下拉只有「不绑定」")
		return nil, nil
	}
	return s.structureTemplateOpts.ListStructureTemplateOptions(ctx, projectID)
}

// errLocaleInvalid 语言清单非法（空清单、重复语言、默认语言未启用、语言码非法）。
var errLocaleInvalid = errors.New("语言清单不合法")

// errLocaleRetireNeeded 禁用语言需要显式确认（审计 I18N-017）：该语言还有已激活路径，
// 直接保存会在访问面留下无人认领的 /en/… 路由。
var errLocaleRetireNeeded = errors.New("禁用语言需要确认")

// ErrLocaleNoTranslations 该语言在 sys_i18n 里没有任何启用中的词条（站点级准入门槛，U1）。
//
// 导出（与其它 project 业务哨兵同形）：读侧（inbound/http 的页面错误归口）要按
// errors.Is 分类成「可展示的业务错误」，而不是落 500 + 归口文案。
var ErrLocaleNoTranslations = errors.New("该语言没有任何界面词条")

// ListLocales 列出站点语言清单（默认语言在前；无记录时返回空切片）。
func (s *Service) ListLocales(ctx context.Context, projectID string) (res []projectdto.LocaleResp, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errLocaleInvalid
	}
	rows, err := s.model.ListLocales(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res = make([]projectdto.LocaleResp, 0, len(rows))
	for _, r := range rows {
		res = append(res, projectdto.LocaleResp{
			Lang: r.Lang, SortOrder: r.SortOrder, IsDefault: r.IsDefault, Enabled: r.Enabled,
		})
	}
	return res, nil
}

// EnabledLangs 返回站点启用语言（默认语言在前，其余按清单顺序）。
//
// 无清单记录（工程刚创建、测试 schema 无本表）时回退「站点默认语言」一种，
// 保证单语言行为与 P3 之前完全一致。
func (s *Service) EnabledLangs(ctx context.Context, projectID string) (langs []string, err error) {
	rows, err := s.model.ListLocales(ctx, projectID)
	if err != nil {
		// 表缺失/查询失败：回退单语言（绝不因语言清单不可读而阻断构建/发布主链）。
		return []string{i18n.GetDefaultLang()}, nil
	}
	for _, r := range rows {
		if r.Enabled {
			langs = append(langs, r.Lang)
		}
	}
	if len(langs) == 0 {
		return []string{i18n.GetDefaultLang()}, nil
	}
	return langs, nil
}

// DefaultLocale 返回站点默认语言：清单里 is_default 那一行，缺失时回退全局默认语言（sys_config 的 i18n 组 default_lang）。
func (s *Service) DefaultLocale(ctx context.Context, projectID string) (lang string, err error) {
	rows, err := s.model.ListLocales(ctx, projectID)
	if err != nil {
		return i18n.GetDefaultLang(), nil
	}
	for _, r := range rows {
		if r.IsDefault {
			return r.Lang, nil
		}
	}
	return i18n.GetDefaultLang(), nil
}

// SaveLocales 全量保存站点语言清单（幂等；同工程全量替换）。
//
// 校验：至少一种语言、语言码白名单（字母/数字/连字符）、语言不重复、
// 至多一个默认语言（未标注时取第一个），默认语言必须启用。
func (s *Service) SaveLocales(ctx context.Context, req *projectdto.LocalesSaveReq) (res []projectdto.LocaleResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" || len(req.Locales) == 0 {
		return nil, errLocaleInvalid
	}
	now := time.Now().UTC()
	seen := map[string]bool{}
	rows := make([]projectmodel.LocaleEntity, 0, len(req.Locales))
	defaultIdx := -1
	for idx, item := range req.Locales {
		lang, lerr := normalizeLangCode(item.Lang)
		if lerr != nil {
			return nil, lerr
		}
		if seen[lang] {
			return nil, errLocaleInvalid
		}
		seen[lang] = true
		enabled := true
		if item.Enabled != nil {
			enabled = *item.Enabled
		}
		if item.IsDefault {
			if defaultIdx >= 0 {
				return nil, errLocaleInvalid
			}
			if !enabled {
				return nil, errLocaleInvalid
			}
			defaultIdx = idx
		}
		rows = append(rows, projectmodel.LocaleEntity{
			ProjectID: req.ProjectID, Lang: lang, SortOrder: idx, IsDefault: false,
			Enabled: enabled, CreatedAt: now, UpdatedAt: now,
		})
	}
	// 未显式标注默认语言时取首个启用语言（避免出现「无默认语言」的清单）。
	if defaultIdx < 0 {
		for i := range rows {
			if rows[i].Enabled {
				defaultIdx = i
				break
			}
		}
		if defaultIdx < 0 {
			return nil, errLocaleInvalid
		}
	}
	rows[defaultIdx].IsDefault = true

	// 站点级准入（U1）：**新增 / 新启用**的语言必须有界面词条，否则拒绝保存。
	//
	// 语义分工（与 U2 的「审核报告 + 一键取消」成对）：
	//   · 这里拦的是「这个语言整体还没准备好」—— 一个词条都没有的语言上线，站点上所有
	//     固定文案都会逐字段回退原文，运营看到的是「语言已启用」，访客看到的是原始语言；
	//   · U2 处理的是「语言准备好了（有词条），但某些页面的内容译文没填」—— 那是内容
	//     层面的缺失，按页面 × 语言逐条呈现并允许把某个页面从该语言撤下来。
	//
	// **只校新增 / 新启用**（第二次改动）：已存在的语言一律放行。否则判据将来一调整
	// （阈值、口径、甚至 sys_i18n 一次误清理），老站点会因为存量数据被**整体拒绝保存**——
	// 用户改不了任何东西，而原因与他正在做的操作无关。判据只作用于「这次新加进来的」。
	//
	// 读不到词条数（库故障）时同样拒绝：放行的后果是静默启用一个空语言，
	// 而拒绝的后果只是「稍后重试」—— 方向取 fail closed。
	beforeEnabled := s.enabledLangsOf(ctx, req.ProjectID)
	wasEnabled := make(map[string]bool, len(beforeEnabled))
	for _, l := range beforeEnabled {
		wasEnabled[l] = true
	}
	for i := range rows {
		if !rows[i].Enabled || wasEnabled[rows[i].Lang] {
			continue
		}
		n, cerr := i18n.CountByLang(ctx, rows[i].Lang)
		switch {
		case errors.Is(cerr, i18n.ErrI18nUnavailable):
			// 「没有接入词条存储」不等于「这个语言没有词条」——这是两种不同的事实：
			//   · 词条存储未接（pkg/i18n 的全局库句柄为空）：只出现在测试进程与精简进程里，
			//     生产装配的 database 组件恒先于本模块就绪；此时放行并留一条日志。
			//     若在这里拒绝，任何没有装配文案存储的调用方都保存不了语言清单
			//     （实测：四个 page feature 用例就是被这条卡住的）。
			//   · 查询本身失败（库在、SQL/连接出错）：下面那支一律拒绝 —— 放行的后果是
			//     静默启用一个空语言，而拒绝的后果只是「稍后重试」。
			logger.Scene("project").With("project", req.ProjectID).With("lang", rows[i].Lang).
				Warn("词条存储未接入，跳过语言准入校验（生产装配下不会出现）")
			continue
		case cerr != nil:
			logger.Scene("project").With("project", req.ProjectID).With("lang", rows[i].Lang).
				Error(cerr, "校验语言词条数失败，本次保存被拒绝")
			return nil, cerr
		}
		if n == 0 {
			logger.Scene("project").With("project", req.ProjectID).With("lang", rows[i].Lang).
				Warn("新增/新启用的语言没有任何启用中的词条，拒绝保存")
			return nil, fmt.Errorf("%w: %s", ErrLocaleNoTranslations, rows[i].Lang)
		}
	}

	// 禁用语言会留下一批**失去归属**的已激活路由（审计 I18N-017）：
	// 访问面还服务着 /en/…，而语言清单里已经没有 en —— 后台再没有任何入口能改它或
	// 下掉它，只能人工登机器删符号链接。所以这是一次需要显式确认的不可逆变更。
	before := s.enabledLangsOf(ctx, req.ProjectID)
	after := map[string]bool{}
	for i := range rows {
		if rows[i].Enabled {
			after[rows[i].Lang] = true
		}
	}
	removed := make([]string, 0, len(before))
	for _, lang := range before {
		if !after[lang] {
			removed = append(removed, lang)
		}
	}
	if len(removed) > 0 && s.retire != nil {
		total, impacted := s.localeRetireImpact(ctx, req.ProjectID, removed)
		if total > 0 && !req.ConfirmRetire {
			return nil, fmt.Errorf("%w：禁用 %s 会让 %d 条已激活路径失去归属，确认后重试",
				errLocaleRetireNeeded, strings.Join(impacted, "、"), total)
		}
	}
	if err = s.model.ReplaceLocales(ctx, req.ProjectID, rows); err != nil {
		return nil, err
	}
	// 清单先落库再下线路由：倒过来会出现「路由已下掉但语言仍在清单里」的中间态，
	// 而那种状态在后台看起来一切正常，只有访问面是坏的。
	if len(removed) > 0 {
		if s.retire == nil {
			logger.Scene("project").With("project", req.ProjectID).
				Warn("语言下线端口未接入，被禁用语言的路由不会被清理")
		} else {
			for _, lang := range removed {
				if _, rerr := s.retire.RetireLocale(ctx, req.ProjectID, lang); rerr != nil {
					return nil, fmt.Errorf("语言 %s 的路由下线失败: %w", lang, rerr)
				}
			}
		}
	}
	return s.ListLocales(ctx, req.ProjectID)
}

// enabledLangsOf 当前启用语言（读取失败返回 nil：调用方按「无变化」处理，不阻断保存）。
func (s *Service) enabledLangsOf(ctx context.Context, projectID string) []string {
	list, err := s.ListLocales(ctx, projectID)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if item.Enabled {
			out = append(out, item.Lang)
		}
	}
	return out
}

// localeRetireImpact 统计这批语言各有几条已激活路径（总数为 0 时不必打扰运营）。
func (s *Service) localeRetireImpact(ctx context.Context, projectID string, langs []string) (total int, impacted []string) {
	for _, lang := range langs {
		n, err := s.retire.LocaleRetireImpact(ctx, projectID, lang)
		if err != nil {
			continue
		}
		if n > 0 {
			total += n
			impacted = append(impacted, lang)
		}
	}
	return total, impacted
}

// normalizeLangCode 语言码白名单校验（与 pipeline.NormalizeLang 同规则：
// 字母/数字/连字符，长度 ≤ 35；project 模块不依赖构建内核，故本地实现）。
func normalizeLangCode(raw string) (string, error) {
	lang := strings.TrimSpace(raw)
	if lang == "" || len(lang) > 35 {
		return "", errLocaleInvalid
	}
	for _, r := range lang {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return "", errLocaleInvalid
		}
	}
	return lang, nil
}
