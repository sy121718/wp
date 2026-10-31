package builder

import (
	"sort"
	"strings"

	"go_wp/internal/builder/core"
)

type fragmentStyleGroup struct {
	id string

	caps []string

	css string
}

func fragmentStyleGroups() []fragmentStyleGroup { return nil }

func fragmentUnstyledCapabilities() map[string]string { return map[string]string{} }

func FragmentStyleCapabilities() []string {
	var out []string
	for _, g := range fragmentStyleGroups() {
		out = append(out, g.caps...)
	}
	sort.Strings(out)
	return out
}

func FragmentUnstyledCapabilities() map[string]string {
	in := fragmentUnstyledCapabilities()
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func fragmentBaseCSSFor(caps htmlFeatures) string {
	if len(caps) == 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	for _, g := range fragmentStyleGroups() {
		if !g.matches(caps) {
			continue
		}
		if strings.TrimSpace(g.css) != "" {
			parts = append(parts, g.css)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (g fragmentStyleGroup) matches(caps htmlFeatures) bool {
	for _, c := range g.caps {
		if _, ok := caps[strings.ToLower(c)]; ok {
			return true
		}
	}
	return false
}

func cartFragmentBaseCSS() string {
	b := core.NewCSSBuckets()
	b.Add(core.BreakpointDesktop, ".sky-cart", []string{
		"display: flex", "flex-direction: column", "gap: 12px", "min-width: 0",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-items", []string{
		"display: flex", "flex-direction: column", "gap: 10px",
		"margin: 0", "padding: 0", "list-style: none",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-item", []string{
		"display: flex", "flex-wrap: wrap", "align-items: center", "gap: 8px", "min-width: 0",
	})

	b.Add(core.BreakpointDesktop, ".sky-cart-item.is-missing", []string{"opacity: 0.65"})
	b.Add(core.BreakpointDesktop, ".sky-cart-name", []string{
		"flex: 1 1 auto", "min-width: 0", "overflow-wrap: anywhere",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-variant", []string{
		"display: block", "font-size: 0.8rem", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-unit", []string{
		"white-space: nowrap", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-qty", []string{
		"display: inline-flex", "align-items: center", "gap: 6px", "margin: 0",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-qty-input", []string{
		"width: min(100%, 72px)", "min-height: 40px", "padding: 6px 8px",
		"border: 1px solid var(--sky-c-border, #d1d5db)", "border-radius: 8px",
		"background: var(--sky-c-surface, #fff)", "color: inherit", "font: inherit",
		"text-align: center",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-qty-submit", []string{
		"min-height: 40px", "padding: 6px 12px",
		"border: 1px solid var(--sky-c-border, #d1d5db)", "border-radius: 8px",
		"background: transparent", "color: inherit", "font: inherit", "cursor: pointer",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-line", []string{"white-space: nowrap", "font-weight: 600"})
	b.Add(core.BreakpointDesktop, ".sky-cart-note", []string{
		"margin: 0", "flex-basis: 100%", "font-size: 0.8rem",
		"color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-total", []string{
		"display: flex", "justify-content: space-between", "gap: 12px",
		"margin: 0", "padding-top: 10px",
		"border-top: 1px solid var(--sky-c-border, #e5e7eb)", "font-weight: 600",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-checkout", []string{
		"display: inline-flex", "align-items: center", "justify-content: center",
		"min-height: 44px", "padding: 10px 18px", "border-radius: 8px",
		"background: var(--sky-c-primary, #2563eb)", "color: #fff",
		"font-weight: 600", "text-decoration: none",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-clear", []string{"margin: 0", "align-self: flex-start"})
	b.Add(core.BreakpointDesktop, ".sky-cart-clear button", []string{
		"padding: 4px 0", "border: 0", "background: none",
		"color: var(--sky-c-text-muted, #6b7280)", "font: inherit", "font-size: 0.85rem",
		"text-decoration: underline", "cursor: pointer",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-empty", []string{
		"margin: 0", "color: var(--sky-c-text-muted, #6b7280)",
	})
	return b.String()
}
