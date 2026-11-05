package pubservice

import (
	"cmp"
	"context"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/net/html"

	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/seo"
)

func (s *Service) refreshFeed(ctx context.Context, projectID, baseURL, dir string, langs []string, defaultLang, siteLangMode string) (err error) {
	if projectID == "" || dir == "" {
		return nil
	}
	if !seo.FeedEnabled() {

		return seo.RemoveFeed(dir)
	}
	routes, err := s.model.ListActiveRoutes(ctx, projectID)
	if err != nil {
		return err
	}
	items := feedItems(dir, baseURL, routes, langs, defaultLang, siteLangMode)
	channel := feedChannel(dir, baseURL, routes, langs, defaultLang, siteLangMode)
	return seo.WriteFeed(dir, channel, items, seo.FeedLimit())
}

func siteLangRule(langs []string, defaultLang, siteLangMode string) (rule publish.LangURLRule, ok bool) {
	if !i18n.SiteLangURLsSeparated(i18n.SiteLangURLMode(siteLangMode)) || len(langs) < 2 {
		return publish.LangURLRule{}, false
	}

	return pipeline.LangURLRuleOf(i18n.SiteLangURLMode(siteLangMode), defaultLang), true
}

func feedItems(dir, baseURL string, routes []pubmodel.RouteEntity, langs []string, defaultLang, siteLangMode string) []seo.FeedItem {
	rule, separate := siteLangRule(langs, defaultLang, siteLangMode)

	separate = separate && strings.TrimSpace(defaultLang) != ""

	cands := make([]pubmodel.RouteEntity, 0, len(routes))
	for _, rt := range routes {
		if rt.PresentationID == nil || strings.TrimSpace(*rt.PresentationID) == "" {
			continue
		}
		if separate {
			lang, _, ok := rule.Locate(rt.Path, langs)
			if !ok || !strings.EqualFold(lang, defaultLang) {
				continue
			}
		}
		cands = append(cands, rt)
	}
	slices.SortStableFunc(cands, func(a, b pubmodel.RouteEntity) int {
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.Path, b.Path)
	})

	items := make([]seo.FeedItem, 0, len(cands))
	for _, rt := range cands {
		file, ok := activeHTMLFile(dir, rt.Path)
		if !ok {
			continue
		}
		doc, ok := readFeedDoc(file)
		if !ok || !doc.isArticle() {
			continue
		}
		items = append(items, seo.FeedItem{
			Title:       doc.Title,
			Link:        seo.JoinURL(baseURL, rt.Path),
			Description: doc.Description,
			PubDate:     rt.UpdatedAt,
		})
	}
	return items
}

func feedChannel(dir, baseURL string, routes []pubmodel.RouteEntity, langs []string, defaultLang, siteLangMode string) seo.FeedChannel {
	ch := seo.FeedChannel{
		Link:     seo.JoinURL(baseURL, "/"),
		Language: strings.TrimSpace(defaultLang),
	}
	for _, p := range homeCandidatePaths(langs, defaultLang, siteLangMode) {
		if ch.Title != "" && ch.Description != "" {
			break
		}
		file, ok := activeHTMLFile(dir, p)
		if !ok {
			continue
		}
		doc, ok := readFeedDoc(file)
		if !ok {
			continue
		}
		if ch.Title == "" {
			ch.Title = doc.Title
		}
		if ch.Description == "" {
			ch.Description = doc.Description
		}
	}
	if ch.Title == "" {
		ch.Title = feedHost(baseURL)
	}
	return ch
}

func homeCandidatePaths(langs []string, defaultLang, siteLangMode string) []string {
	out := make([]string, 0, 3)
	if rule, ok := siteLangRule(langs, defaultLang, siteLangMode); ok && strings.TrimSpace(defaultLang) != "" {
		if p, err := rule.Path(defaultLang, "/"); err == nil {
			out = append(out, p)
		}
	}
	out = append(out, "/index", "/")

	seen := make(map[string]bool, len(out))
	uniq := make([]string, 0, len(out))
	for _, p := range out {
		if seen[p] {
			continue
		}
		seen[p] = true
		uniq = append(uniq, p)
	}
	return uniq
}

func feedHost(baseURL string) string {
	raw := strings.TrimSpace(baseURL)
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return strings.TrimRight(raw, "/")
}

type feedDoc struct {
	Title string

	Description string

	Canonical string

	Types []string
}

var feedArticleTypes = map[string]bool{
	"article": true, "newsarticle": true, "blogposting": true, "techarticle": true,
	"socialmediaposting": true, "report": true, "scholarlyarticle": true,
}

func (d feedDoc) isArticle() bool {
	for _, t := range d.Types {
		name := strings.ToLower(strings.TrimSpace(t))
		name = strings.TrimPrefix(name, "https://schema.org/")
		name = strings.TrimPrefix(name, "http://schema.org/")
		if feedArticleTypes[name] || strings.HasSuffix(name, "article") {
			return true
		}
	}
	return false
}

func parseFeedDoc(r io.Reader) (doc feedDoc, err error) {
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		switch tok.Data {
		case "title":

			if doc.Title == "" && z.Next() == html.TextToken {
				doc.Title = strings.TrimSpace(z.Token().Data)
			}
		case "meta":
			if strings.EqualFold(attr(tok, "name"), "description") && doc.Description == "" {
				doc.Description = strings.TrimSpace(attr(tok, "content"))
			}
		case "link":
			if strings.EqualFold(attr(tok, "rel"), "canonical") && doc.Canonical == "" {
				doc.Canonical = strings.TrimSpace(attr(tok, "href"))
			}
		case "script":
			if !strings.EqualFold(strings.TrimSpace(attr(tok, "type")), "application/ld+json") {
				continue
			}
			if z.Next() != html.TextToken {
				continue
			}
			doc.Types = append(doc.Types, jsonLDTypes(z.Token().Data)...)
		}
	}
	return doc, nil
}

func jsonLDTypes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var root any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil
	}
	var out []string
	collectLDTypes(root, &out)
	return out
}

func collectLDTypes(node any, out *[]string) {
	switch v := node.(type) {
	case []any:
		for _, item := range v {
			collectLDTypes(item, out)
		}
	case map[string]any:
		if t, ok := v["@type"]; ok {
			collectLDTypes(t, out)
		}
		if g, ok := v["@graph"]; ok {
			collectLDTypes(g, out)
		}
	case string:
		if s := strings.TrimSpace(v); s != "" {
			*out = append(*out, s)
		}
	}
}

func readFeedDoc(file string) (doc feedDoc, ok bool) {
	f, err := os.Open(file)
	if err != nil {
		return feedDoc{}, false
	}
	defer f.Close()
	parsed, perr := parseFeedDoc(f)
	if perr != nil {
		return feedDoc{}, false
	}
	return parsed, true
}

func activeHTMLFile(dir, path string) (string, bool) {
	rel := strings.Trim(strings.TrimSpace(path), "/")

	if rel == "" {
		rel = "index"
	}
	cands := []string{
		filepath.Join(dir, filepath.FromSlash(rel), "index.html"),
		filepath.Join(dir, filepath.FromSlash(rel)),
	}
	for _, cand := range cands {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand, true
		}
	}
	return "", false
}

func (s *Service) RefreshSiteFiles(ctx context.Context, projectID, baseURL, dir string, langs []string, defaultLang, siteLangMode, notFoundHTML string) (err error) {
	if projectID == "" || dir == "" {
		return nil
	}

	if nerr := publish.SyncNotFoundPage(dir, notFoundHTML); nerr != nil {
		logger.Scene("publication").With("dir", dir).Error(nerr, "自定义 404 页刷新失败")
		return nerr
	}
	paths, err := s.model.ListActivePaths(ctx, projectID)
	if err != nil {
		return err
	}
	if err = seo.WriteSiteFiles(dir, baseURL, sitemapEntries(baseURL, paths, langs, defaultLang, siteLangMode)); err != nil {
		return err
	}

	if ferr := s.refreshFeed(ctx, projectID, baseURL, dir, langs, defaultLang, siteLangMode); ferr != nil {
		logger.Scene("publication").With("dir", dir).Error(ferr, "feed.xml 刷新失败")
		return ferr
	}
	return nil
}

func sitemapEntries(baseURL string, paths, langs []string, defaultLang, siteLangMode string) []seo.SitemapEntry {
	if !i18n.SiteLangURLsSeparated(i18n.SiteLangURLMode(siteLangMode)) || len(langs) < 2 {
		return seo.EntriesFromPaths(baseURL, paths)
	}

	rule, _ := siteLangRule(langs, defaultLang, siteLangMode)
	byLogical := map[string]map[string]string{}
	logicalOf := map[string]string{}
	for _, p := range paths {
		lang, logical, ok := rule.Locate(p, langs)
		if !ok {
			continue
		}
		if byLogical[logical] == nil {
			byLogical[logical] = map[string]string{}
		}
		byLogical[logical][lang] = p
		logicalOf[p] = logical
	}
	out := make([]seo.SitemapEntry, 0, len(paths))
	for _, p := range paths {
		entry := seo.EntryForPath(baseURL, p)
		logical, ok := logicalOf[p]
		if !ok {
			out = append(out, entry)
			continue
		}
		group := byLogical[logical]
		if len(group) < 2 {
			out = append(out, entry)
			continue
		}
		for _, l := range langs {
			alt, exists := group[l]
			if !exists {
				continue
			}
			entry.Alternates = append(entry.Alternates, seo.SitemapAlternate{
				Lang: l, Href: seo.JoinURL(baseURL, alt),
			})
		}
		if alt, exists := group[defaultLang]; exists {
			entry.Alternates = append(entry.Alternates, seo.SitemapAlternate{
				Lang: "x-default", Href: seo.JoinURL(baseURL, alt),
			})
		}
		out = append(out, entry)
	}
	return out
}

const (
	AuditLevelError   = "error"
	AuditLevelWarning = "warning"
	AuditLevelInfo    = "info"
)

const (
	AuditTitleMissing       = "title_missing"
	AuditTitleDuplicate     = "title_duplicate"
	AuditDescriptionMissing = "description_missing"
	AuditCanonicalMissing   = "canonical_missing"
	AuditImageAltMissing    = "image_alt_missing"
	AuditInternalLinkBroken = "internal_link_broken"
	AuditHreflangIncomplete = "hreflang_incomplete"
)

type AuditDocument struct {
	Path        string
	Title       string
	Description string
	Canonical   string
	ImageSrcs   []string
	Links       []string
	Hreflangs   []string
}

func ParseAuditDocument(path string, r io.Reader) (doc AuditDocument, err error) {
	doc.Path = path
	z := html.NewTokenizer(r)
	collectAlt := false
	var imgSrc string
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		switch tok.Data {
		case "title":
			if z.Next() == html.TextToken {
				doc.Title = strings.TrimSpace(z.Token().Data)
			}
		case "meta":
			name := attr(tok, "name")
			if strings.EqualFold(name, "description") {
				doc.Description = strings.TrimSpace(attr(tok, "content"))
			}
		case "link":
			rel := strings.ToLower(attr(tok, "rel"))
			switch {
			case rel == "canonical":
				doc.Canonical = strings.TrimSpace(attr(tok, "href"))
			case rel == "alternate":
				if h := strings.TrimSpace(attr(tok, "hreflang")); h != "" {
					doc.Hreflangs = append(doc.Hreflangs, h)
				}
			}
		case "img":
			imgSrc = strings.TrimSpace(attr(tok, "src"))
			if strings.TrimSpace(attr(tok, "alt")) == "" && imgSrc != "" {
				doc.ImageSrcs = append(doc.ImageSrcs, imgSrc)
			}
			collectAlt = false
		case "a":
			if href := strings.TrimSpace(attr(tok, "href")); isInternalLink(href) {
				doc.Links = append(doc.Links, href)
			}
		}
		_ = collectAlt
	}
	return doc, nil
}

func attr(tok html.Token, key string) string {
	for _, a := range tok.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func isInternalLink(href string) bool {
	if href == "" || !strings.HasPrefix(href, "/") || strings.HasPrefix(href, "//") {
		return false
	}
	return true
}

func AuditSite(docs []AuditDocument, activePaths map[string]bool) (issues []AuditIssue) {
	byTitle := map[string][]string{}
	for _, d := range docs {
		if d.Title == "" {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditTitleMissing, Level: AuditLevelError})
		} else {
			byTitle[d.Title] = append(byTitle[d.Title], d.Path)
		}
		if d.Description == "" {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditDescriptionMissing, Level: AuditLevelWarning})
		}
		if d.Canonical == "" {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditCanonicalMissing, Level: AuditLevelWarning})
		}
		for _, src := range d.ImageSrcs {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditImageAltMissing, Level: AuditLevelWarning,
				Params: AuditIssueParams{Src: src}})
		}
		for _, href := range d.Links {
			if !linkExists(href, activePaths) {
				issues = append(issues, AuditIssue{Path: d.Path, Item: AuditInternalLinkBroken, Level: AuditLevelError,
					Params: AuditIssueParams{Href: href}})
			}
		}

		if len(d.Hreflangs) == 1 {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditHreflangIncomplete, Level: AuditLevelWarning})
		}
	}

	titles := make([]string, 0, len(byTitle))
	for title := range byTitle {
		titles = append(titles, title)
	}
	slices.Sort(titles)
	for _, title := range titles {
		pages := byTitle[title]
		if len(pages) < 2 {
			continue
		}
		slices.Sort(pages)

		issues = append(issues, AuditIssue{Item: AuditTitleDuplicate, Level: AuditLevelWarning,
			Params: AuditIssueParams{Title: title, Paths: append([]string(nil), pages...)}})
	}
	return issues
}

func linkExists(href string, activePaths map[string]bool) bool {
	if len(activePaths) == 0 {
		return true
	}
	p := href
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if p == "" || p == "/" {
		p = "/"
	}
	for _, cand := range []string{p, strings.TrimSuffix(p, "/"), p + "/"} {
		if cand != "" && activePaths[cand] {
			return true
		}
	}
	return false
}

func CollectActiveDocuments(dir string, activePaths []string) (docs []AuditDocument, err error) {
	if strings.TrimSpace(dir) == "" || len(activePaths) == 0 {
		return nil, nil
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		return nil, nil
	}
	for _, access := range activePaths {
		file, ok := activeHTMLFile(dir, access)
		if !ok {
			continue
		}
		f, oerr := os.Open(file)
		if oerr != nil {
			continue
		}
		doc, perr := ParseAuditDocument(access, f)
		f.Close()
		if perr == nil {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

func (s *Service) RunSEOAudit(ctx context.Context, projectID string) (AuditResult, error) {
	if strings.TrimSpace(projectID) == "" {
		return AuditResult{}, pubenums.ErrProjectMissing()
	}
	paths, err := s.model.ListActivePaths(ctx, projectID)
	if err != nil {
		return AuditResult{}, err
	}
	if len(paths) == 0 {
		return newAuditResult(0, nil), nil
	}
	active := make(map[string]bool, len(paths))
	for _, p := range paths {
		active[p] = true
	}
	docs, err := CollectActiveDocuments(publish.ActiveRoot(), paths)
	if err != nil {
		return AuditResult{}, err
	}
	mine := make([]AuditDocument, 0, len(docs))
	for _, d := range docs {
		if active[d.Path] {
			mine = append(mine, d)
		}
	}
	return newAuditResult(len(mine), AuditSite(mine, active)), nil
}
