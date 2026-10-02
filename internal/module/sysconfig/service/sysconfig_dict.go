package sysconfigservice

// sysconfig_dict.go — 读路径：字典下拉选项（sys_dict / sys_area）。
//
// 这是数据字典的第一个消费方：系统设置页的四个下拉里，三个（默认语言 / 默认货币 /
// 默认国家）的选项来自这里。本批没有字典编辑能力 —— 选项由迁移 seed，改动走迁移。

import (
	"context"
	"strings"

	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
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
		res = append(res, sysconfigdto.DictOption{Code: r.Code, Label: label})
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
	zh := strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh")
	res = make([]sysconfigdto.CountryOption, 0, len(rows))
	for i := range rows {
		r := rows[i]
		label := r.NameEn
		if zh {
			label = r.NameZh
		}
		if strings.TrimSpace(label) == "" {
			label = r.Code // 两列都缺时用 code：宁可显示 AO，也不要空行
		}
		res = append(res, sysconfigdto.CountryOption{Code: r.Code, Label: label})
	}
	return res, nil
}
