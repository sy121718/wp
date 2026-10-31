package i18nvalues

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

type Adapter struct {
	reader sysconfigcontract.ConfigReader
}

func New(reader sysconfigcontract.ConfigReader) Adapter { return Adapter{reader: reader} }

var _ i18n.ValueLoader = Adapter{}.Load

func (a Adapter) Load(ctx context.Context) (i18n.RuntimeValues, error) {
	if a.reader == nil {
		return i18n.RuntimeValues{}, errors.New("sysconfig 配置读取口未注入")
	}
	group, err := a.reader.GetGroup(ctx, sysconfigcontract.GroupI18n)
	if err != nil {
		if errors.Is(err, sysconfigcontract.ErrGroupNotFound) {
			return i18n.RuntimeValues{}, nil
		}
		return i18n.RuntimeValues{}, err
	}
	if group == nil {
		return i18n.RuntimeValues{}, nil
	}
	values, problems := RuntimeValuesOf(group.Data)

	if trade, terr := a.reader.GetGroup(ctx, sysconfigcontract.GroupTrade); terr == nil && trade != nil {
		if v, ok := trade.Data["default_country"].(string); ok {
			values.DefaultCountry = strings.TrimSpace(v)
		}
		if v, ok := trade.Data["default_currency"].(string); ok {
			values.DefaultCurrency = strings.TrimSpace(v)
		}
	} else if terr != nil && !errors.Is(terr, sysconfigcontract.ErrGroupNotFound) {
		return i18n.RuntimeValues{}, terr
	}

	for _, p := range problems {
		logger.Scene("i18n").With("group", sysconfigcontract.GroupI18n).Warn(p)
	}
	return values, nil
}

func RuntimeValuesOf(data map[string]any) (vals i18n.RuntimeValues, problems []string) {
	vals.DefaultLang = stringValueOf(data, sysconfigcontract.KeyDefaultLang, &problems)
	vals.SiteLangURLMode = stringValueOf(data, sysconfigcontract.KeySiteLangURLMode, &problems)
	vals.LangURLCodes = urlCodesValueOf(data, sysconfigcontract.KeyLangURLCodes, &problems)
	sort.Strings(problems)
	return vals, problems
}

func stringValueOf(data map[string]any, key string, problems *[]string) string {
	raw, ok := data[key]
	if !ok || raw == nil {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		*problems = append(*problems, fmt.Sprintf("%s 取值类型不是字符串（%T），该键按未配置处理", key, raw))
		return ""
	}
	return strings.TrimSpace(s)
}

func urlCodesValueOf(data map[string]any, key string, problems *[]string) map[string]string {
	raw, ok := data[key]
	if !ok || raw == nil {
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		*problems = append(*problems, fmt.Sprintf("%s 取值类型不是对象（%T），该键按未配置处理", key, raw))
		return nil
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		code, ok := v.(string)
		if !ok || strings.TrimSpace(code) == "" {
			*problems = append(*problems, fmt.Sprintf("%s.%s 取值非法（%v），已忽略该项", key, k, v))
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(code)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
