package money

import "strconv"

func FormatAudit(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

func FormatYuan(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func CentsToYuanText(cents int64) string {
	return FormatYuan(float64(cents) / 100)
}

func FormatYuanOrDash(v *float64) string {
	if v == nil {
		return "—"
	}
	return FormatYuan(*v)
}
