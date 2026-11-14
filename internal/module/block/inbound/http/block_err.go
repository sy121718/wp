package blockhttp

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	blockcontract "go_wp/internal/module/block/contract"
	blockenums "go_wp/internal/module/block/enums"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

const blockRefDetailMaxBytes = 420

const blockRefTextMaxBytes = 512

func blockUsageKindKey(kind blockcontract.BlockUsageKind) string {
	switch kind {
	case blockcontract.UsageKindPageDocument:
		return blockenums.MsgBlockUsagePageDocument
	case blockcontract.UsageKindPageStructure:
		return blockenums.MsgBlockUsagePageStructure
	case blockcontract.UsageKindPageRevision:
		return blockenums.MsgBlockUsagePageRevision
	case blockcontract.UsageKindThemeSlot:
		return blockenums.MsgBlockUsageThemeSlot
	case blockcontract.UsageKindBlockDocument:
		return blockenums.MsgBlockUsageBlockDocument
	case blockcontract.UsageKindContentTemplate:
		return blockenums.MsgBlockUsageContentTemplate
	case blockcontract.UsageKindPresentationInstance:
		return blockenums.MsgBlockUsagePresentationInstance
	default:
		return string(kind)
	}
}

func blockRefUsageText(c *gin.Context, err error) string {
	usages := blockcontract.BlockUsages(err)
	if len(usages) == 0 {
		return ""
	}

	tr := func(key, fallback string) string {
		return response.MsgText(c, facing.Msg{Key: key, Fallback: fallback})
	}
	order := make([]blockcontract.BlockUsageKind, 0, len(usages))
	grouped := map[blockcontract.BlockUsageKind][]string{}
	for _, u := range usages {
		loc := strings.TrimSpace(u.Label)
		if loc == "" {
			loc = strings.TrimSpace(u.EntityID)
		}
		if detail := strings.TrimSpace(u.Detail); detail != "" {
			loc = loc + "（" + detail + "）"
		}
		if _, ok := grouped[u.Kind]; !ok {
			order = append(order, u.Kind)
		}
		grouped[u.Kind] = append(grouped[u.Kind], loc)
	}
	parts := make([]string, 0, len(order))
	for _, kind := range order {
		key := blockUsageKindKey(kind)
		locs := grouped[kind]
		shown := locs
		tail := ""
		if len(locs) > 3 {
			shown = locs[:3]
			tail = response.FillText(c, blockenums.UsageMore, " 等 {count} 处",
				map[string]string{"count": strconv.Itoa(len(locs))})
		}
		parts = append(parts, tr(key, key)+"："+strings.Join(shown, "、")+tail)
	}
	return truncateRunes(strings.Join(parts, "；"), blockRefDetailMaxBytes)
}

func truncateRunes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
