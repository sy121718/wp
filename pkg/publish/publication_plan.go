package publish

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"
)

// PublicationPlan 是本次发布冻结的站点语言与引用快照。
type PublicationPlan struct {
	SiteLangs []string `json:"siteLangs"`

	DefaultLang string `json:"defaultLang"`

	References *ReferenceSnapshot `json:"references,omitempty"`
}

func (p PublicationPlan) Empty() bool {
	return len(p.normalizedLangs()) == 0 && strings.TrimSpace(p.DefaultLang) == ""
}

func (p PublicationPlan) Normalize() PublicationPlan {
	return PublicationPlan{
		SiteLangs:   p.normalizedLangs(),
		DefaultLang: strings.TrimSpace(p.DefaultLang),
		References:  p.References.Clone(),
	}
}

func (p PublicationPlan) normalizedLangs() []string {
	if len(p.SiteLangs) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.SiteLangs))
	seen := make(map[string]bool, len(p.SiteLangs))
	for _, raw := range p.SiteLangs {
		lang := strings.TrimSpace(raw)
		if lang == "" || seen[lang] {
			continue
		}
		seen[lang] = true
		out = append(out, lang)
	}
	return out
}

func (p PublicationPlan) Hash() string {
	data, err := json.Marshal(p.Normalize())
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (p PublicationPlan) String() string {
	n := p.Normalize()
	if n.Empty() {
		return "(未冻结)"
	}
	return fmt.Sprintf("siteLangs=%s default=%s", strings.Join(n.SiteLangs, ","), n.DefaultLang)
}
