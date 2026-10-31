package projectservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectenums "go_wp/internal/module/project/enums"
	projectmodel "go_wp/internal/module/project/model"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

var _ projectcontract.ProjectService = (*Service)(nil)

type Service struct {
	model *projectmodel.Model

	retire projectcontract.LocaleRetirePort

	structureTemplateOpts projectcontract.StructureTemplateOptionsPort
}

func (s *Service) SetStructureTemplateOptionsPort(p projectcontract.StructureTemplateOptionsPort) {
	s.structureTemplateOpts = p
}

func (s *Service) SetLocaleRetirePort(port projectcontract.LocaleRetirePort) { s.retire = port }

func NewService(model *projectmodel.Model) *Service {
	return &Service{model: model}
}

var (
	ErrProjectNotFound = projectenums.Err(projectenums.ErrProjectNotFound)
	ErrInvalidName     = projectenums.Err(projectenums.ErrInvalidName)
	ErrInvalidSettings = projectenums.Err(projectenums.ErrInvalidSettings)
	ErrInvalidParam    = projectenums.Err(projectenums.ErrInvalidParam)
)

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

	if _, terr := s.ensureDefaultTheme(ctx, e.ID); terr != nil {
		logger.Scene("project").With("project", e.ID).Error(terr, "创建默认主题失败")
	}
	return toResp(e), nil
}

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

var _ projectcontract.ShippingPolicyReader = (*Service)(nil)

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

		logger.Scene("project").With("project", id).Error(nerr,
			"站点运费配置非法（负数或超上限），本次结算按不收运费处理；请到站点设置页改正")
		return projectdto.ShippingPolicy{}, nil
	}
	return policy, nil
}

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

var errLocaleInvalid = errors.New("语言清单不合法")

var errLocaleRetireNeeded = errors.New("禁用语言需要确认")

var ErrLocaleNoTranslations = errors.New("该语言没有任何界面词条")

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

func (s *Service) EnabledLangs(ctx context.Context, projectID string) (langs []string, err error) {
	rows, err := s.model.ListLocales(ctx, projectID)
	if err != nil {

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
