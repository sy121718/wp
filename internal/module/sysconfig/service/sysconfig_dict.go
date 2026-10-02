package sysconfigservice

// sysconfig_dict.go — 读路径：字典下拉选项（sys_dict / sys_area）。
//
// 这是数据字典的第一个消费方：系统设置页的四个下拉里，三个（默认语言 / 默认货币 /
// 默认国家）的选项来自这里。本批没有字典编辑能力 —— 选项由迁移 seed，改动走迁移。

import (
	"context"
	"strings"
	"time"

	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	"go_wp/pkg/logger"
)

// ListDictOptions 列出某类型字典的启用项（按 sort_order，稳定）。
//
// Label 这样拼：语言用 code 本身（zh-CN 就是它自己的名字，URL 短码另行展示在备注列），
// 货币用「CODE 符号」（运营认符号比认三字母码快）。
func (s *Service) ListDictOptions(ctx context.Context, dictType string) (res []sysconfigdto.DictOption, err error) {
	rows, err := s.m.ListDictItems(ctx, strings.TrimSpace(dictType))
	if err != nil {
		return nil, err
	}
	res = make([]sysconfigdto.DictOption, 0, len(rows))
	for i := range rows {
		r := rows[i]
		label := r.Code
		if dictType == sysconfigmodel.DictTypeCurrency {
			if sym := strings.TrimSpace(r.Symbol); sym != "" {
				label = r.Code + " " + sym
			}
		} else if url := strings.TrimSpace(r.URLCode); url != "" && url != r.Code {
			// 语言：短码与 code 不同时带上（运营要能看出 URL 里会出现什么）。
			label = r.Code + "（/" + url + "）"
		}
		res = append(res, sysconfigdto.DictOption{Code: r.Code, Label: label, UIAvailable: r.UIAvailable})
	}
	return res, nil
}

// ListCountryOptions 列出启用国家（label 按界面语言取 name_zh / name_en）。
//
// lang 只区分「以中文开头即中文名，否则英文名」：这不是语言解析，只是挑一列 ——
// 国家名的完整本地化要等字典表补齐多语言列，现在多写一层判断没有依据。
func (s *Service) ListCountryOptions(ctx context.Context, lang string) (res []sysconfigdto.CountryOption, err error) {
	rows, err := s.m.ListAreasByKind(ctx, sysconfigmodel.AreaKindCountry)
	if err != nil {
		return nil, err
	}
	zh := isChineseLang(lang)
	res = make([]sysconfigdto.CountryOption, 0, len(rows))
	for i := range rows {
		r := rows[i]
		res = append(res, sysconfigdto.CountryOption{Code: r.Code, Label: countryNameOf(r, zh)})
	}
	return res, nil
}

// CountryLabel 把国家/地区代码换成给定界面语言下的显示名（查不到回落 code）。
//
// 缓存形状是「归一语言 → (码 → 名)」：订单详情一次渲染可能解析收货与账单两个地址，
// 而列表页一屏有几十行 —— 不缓存的话每行都要把 sys_area 拉一遍。
// 读失败**不写缓存**（下一次渲染重试），并只记日志不改返回：这里返回的是一行展示文字，
// 读不到字典的正确表现是「显示代码」，不是让整个页面失败。
func (s *Service) CountryLabel(ctx context.Context, lang, code string) string {
	code = strings.TrimSpace(code)
	if code == "" || s == nil || s.m == nil {
		return code
	}
	key := normalizeLabelLang(lang)
	if index, ok := s.countryIndex(key); ok {
		return labelFromIndex(index, code)
	}
	rows, err := s.m.ListAreasByKind(ctx, sysconfigmodel.AreaKindCountry)
	if err != nil {
		logger.Scene("sysconfig").Error(err, "国家名读取失败，本次回落显示国家代码")
		return code
	}
	zh := key == labelLangZh
	index := make(map[string]string, len(rows))
	for i := range rows {
		rowCode := strings.TrimSpace(rows[i].Code)
		if rowCode == "" {
			continue
		}
		// 键统一大写：快照里的 code 由收参层归一过，但 API 直传或存量数据可能是小写，
		// 差一个大小写就查不到名字 —— 那是「同一个码有时显示 CN、有时显示中国」的来源。
		index[strings.ToUpper(rowCode)] = countryNameOf(rows[i], zh)
	}
	s.countryLabels.Store(key, countryLabelEntry{index: index, expires: time.Now().Add(countryLabelTTL)})
	return labelFromIndex(index, code)
}

// countryIndex 取未过期的「码 → 名」索引；未命中或已过期返回 ok=false（由调用方重建）。
//
// 过期**就地重建**、不加锁：TTL 到期那一瞬可能有几个并发请求同时重建，每个各做一次
// 247 行的全表读 —— 代价可忽略（每 5 分钟最多一次），换掉一把锁是划算的，
// 也维持了 sync.Map「读远多于写」的设计前提。
func (s *Service) countryIndex(key string) (map[string]string, bool) {
	v, ok := s.countryLabels.Load(key)
	if !ok {
		return nil, false
	}
	entry, ok := v.(countryLabelEntry)
	if !ok || !entry.valid(time.Now()) {
		return nil, false
	}
	return entry.index, true
}

// valid 条目在 now 时刻是否仍然可用（过期即失效）。
//
// 抽成方法而不是内联在 countryIndex 里：过期判断是本缓存**唯一的失效路径**，
// 它必须能被单独钉住 —— 内联在查库流程里就只能靠「等 5 分钟」来验证，那进不了回归。
func (e countryLabelEntry) valid(now time.Time) bool {
	return now.Before(e.expires)
}

// labelFromIndex 从码索引里取显示名；缺行（或值为空）回落 code。
func labelFromIndex(index map[string]string, code string) string {
	if label, ok := index[strings.ToUpper(strings.TrimSpace(code))]; ok && strings.TrimSpace(label) != "" {
		return label
	}
	return code
}

// countryNameOf 一行的显示名：中文界面取 name_zh，否则 name_en；两列都缺时用 code
// （宁可显示 AO，也不要空行 / 空地址段）。
func countryNameOf(r sysconfigmodel.SysAreaEntity, zh bool) string {
	label := r.NameEn
	if zh {
		label = r.NameZh
	}
	if strings.TrimSpace(label) == "" {
		label = r.Code
	}
	return label
}

// 归一后的语言键。**只有两档**，与 ListCountryOptions 的判据同源（字典表当前只有
// name_zh / name_en 两列）：不归一的话 zh-CN 与 zh-TW 会各查一次库、各缓存一份完全相同的清单。
const (
	labelLangZh = "zh"
	labelLangEn = "en"
)

// normalizeLabelLang 把界面语言归一到上面两档。
func normalizeLabelLang(lang string) string {
	if isChineseLang(lang) {
		return labelLangZh
	}
	return labelLangEn
}

// isChineseLang 判「以 zh 开头」（zh / zh-CN / zh-Hant 都算中文界面）。
func isChineseLang(lang string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh")
}
