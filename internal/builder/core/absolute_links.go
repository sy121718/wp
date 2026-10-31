package core

import (
	"regexp"
	"strings"

	"go_wp/internal/seo"
	"go_wp/pkg/upload"
)

var richRootRelativeAttrRe = regexp.MustCompile(`(?i)\b(href|src)="(/[^"]*)"`)

func AbsolutizeInternalLinks(html string) string {
	if html == "" || (seo.SiteBaseURL() == "" && upload.BaseURL() == "") || !strings.Contains(html, "=\"/") {
		return html
	}
	return richRootRelativeAttrRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := richRootRelativeAttrRe.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		if strings.HasPrefix(sub[2], "//") {
			return m
		}
		if strings.EqualFold(sub[1], "src") && strings.HasPrefix(sub[2], "/storage/") {
			return sub[1] + "=\"" + upload.StorageURL(sub[2]) + "\""
		}
		return sub[1] + "=\"" + seo.AbsoluteSiteURL(sub[2]) + "\""
	})
}
