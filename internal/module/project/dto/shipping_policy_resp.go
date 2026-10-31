package projectdto

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const MaxShippingFeeCents int64 = 1_000_000

var (
	ErrShippingFeeFormat = errors.New("运费金额必须是不小于 0 的数字，最多两位小数")

	ErrShippingFeeNegative = errors.New("运费金额不能为负数")

	ErrShippingFeeTooLarge = errors.New("运费金额超出上限")
)

type ShippingPolicy struct {
	BaseFeeCents int64

	FreeThresholdCents int64
}

func NormalizeShippingPolicy(baseCents, thresholdCents int64) (p ShippingPolicy, err error) {
	if baseCents < 0 || thresholdCents < 0 {
		return ShippingPolicy{}, ErrShippingFeeNegative
	}
	if baseCents > MaxShippingFeeCents || thresholdCents > MaxShippingFeeCents {
		return ShippingPolicy{}, ErrShippingFeeTooLarge
	}
	return ShippingPolicy{BaseFeeCents: baseCents, FreeThresholdCents: thresholdCents}, nil
}

func ParseYuanToCents(raw string) (cents int64, err error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, nil
	}
	if strings.HasPrefix(s, "-") {
		return 0, ErrShippingFeeNegative
	}
	intPart, fracPart, hasDot := strings.Cut(s, ".")
	if !isASCIIDigits(intPart) || (hasDot && !isASCIIDigits(fracPart)) {
		return 0, ErrShippingFeeFormat
	}
	if intPart == "" && fracPart == "" {
		return 0, ErrShippingFeeFormat
	}
	if len(fracPart) > 2 {
		return 0, ErrShippingFeeFormat
	}
	yuan := int64(0)
	if intPart != "" {
		parsed, perr := strconv.ParseInt(intPart, 10, 64)
		if perr != nil {

			return 0, ErrShippingFeeTooLarge
		}
		yuan = parsed
	}

	if yuan > MaxShippingFeeCents/100 {
		return 0, ErrShippingFeeTooLarge
	}
	frac := fracPart
	for len(frac) < 2 {
		frac += "0"
	}
	fracCents, ferr := strconv.ParseInt(frac, 10, 64)
	if ferr != nil {
		return 0, ErrShippingFeeFormat
	}
	cents = yuan*100 + fracCents
	if cents > MaxShippingFeeCents {
		return 0, ErrShippingFeeTooLarge
	}
	return cents, nil
}

func FormatCentsAsYuan(cents int64) string {
	if cents <= 0 {
		return ""
	}
	return strconv.FormatInt(cents/100, 10) + "." + fmt.Sprintf("%02d", cents%100)
}

func isASCIIDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
