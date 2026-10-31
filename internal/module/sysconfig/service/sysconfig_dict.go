package sysconfigservice

import (
	"context"
	"strings"
	"time"

	sysconfigdto "go_wp/internal/module/sysconfig/dto"
	sysconfigmodel "go_wp/internal/module/sysconfig/model"
	"go_wp/pkg/logger"
)

func (s *Service) ListDictOptions(ctx context.Context, dictType string) (res []sysconfigdto.DictOption, err error) {
	rows, err := s.m.ListDictItems(ctx, strings.TrimSpace(dictType))
	if err != nil {
		return nil, err
	}
	res = make([]sysconfigdto.DictOption, 0, len(rows))
	for i := range rows {
		r := rows[i]
		label := r.Code
		symbol := ""
		if dictType == sysconfigmodel.DictTypeCurrency {
			if sym := strings.TrimSpace(r.Symbol); sym != "" {
				label = r.Code + " " + sym
				symbol = sym
			}
		} else if url := strings.TrimSpace(r.URLCode); url != "" && url != r.Code {

			label = r.Code + "（/" + url + "）"
		}
		res = append(res, sysconfigdto.DictOption{Code: r.Code, Label: label, Symbol: symbol, UIAvailable: r.UIAvailable})
	}
	return res, nil
}

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

		index[strings.ToUpper(rowCode)] = countryNameOf(rows[i], zh)
	}
	s.countryLabels.Store(key, countryLabelEntry{index: index, expires: time.Now().Add(countryLabelTTL)})
	return labelFromIndex(index, code)
}

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

func (e countryLabelEntry) valid(now time.Time) bool {
	return now.Before(e.expires)
}

func labelFromIndex(index map[string]string, code string) string {
	if label, ok := index[strings.ToUpper(strings.TrimSpace(code))]; ok && strings.TrimSpace(label) != "" {
		return label
	}
	return code
}

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

const (
	labelLangZh = "zh"
	labelLangEn = "en"
)

func normalizeLabelLang(lang string) string {
	if isChineseLang(lang) {
		return labelLangZh
	}
	return labelLangEn
}

func isChineseLang(lang string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh")
}
