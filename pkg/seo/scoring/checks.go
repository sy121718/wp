package scoring

type checkFunc func(*Input) (int, string)

type Check struct {
	Key   string
	Score checkFunc
	Max   int
}

type Section struct {
	Key    string
	Weight float64
	Checks []Check
}

func allSections() []Section {
	return []Section{
		{Key: "title", Weight: 0.15, Checks: []Check{
			{Key: "title_present", Max: 5, Score: chkTitlePresent},
			{Key: "title_length", Max: 5, Score: chkTitleLength},
			{Key: "title_keyword_first_half", Max: 5, Score: chkTitleKeywordFirstHalf},
		}},
		{Key: "meta", Weight: 0.05, Checks: []Check{
			{Key: "meta_length", Max: 3, Score: chkMetaLength},
			{Key: "meta_has_cta", Max: 2, Score: chkMetaCTA},
		}},
		{Key: "headings", Weight: 0.10, Checks: []Check{
			{Key: "single_h1", Max: 4, Score: chkSingleH1},
			{Key: "h1_has_keyword", Max: 3, Score: chkH1Keyword},
			{Key: "heading_hierarchy", Max: 3, Score: chkHierarchy},
		}},
		{Key: "content", Weight: 0.25, Checks: []Check{

			{Key: "content_length", Max: 12, Score: chkContentLength},
			{Key: "paragraph_length", Max: 4, Score: chkParagraphLength},
			{Key: "sentence_length", Max: 4, Score: chkSentenceLength},
			{Key: "comparison_table", Max: 5, Score: chkComparisonTable},
		}},
		{Key: "keywords", Weight: 0.15, Checks: []Check{
			{Key: "keyword_density", Max: 8, Score: chkKeywordDensity},
			{Key: "keyword_positions", Max: 5, Score: chkKeywordPositions},
			{Key: "secondary_keywords", Max: 2, Score: chkSecondaryKeywords},
		}},
		{Key: "links", Weight: 0.10, Checks: []Check{
			{Key: "internal_link_count", Max: 6, Score: chkInternalLinks},
			{Key: "external_links", Max: 2, Score: chkExternalLinks},
			{Key: "anchor_descriptive", Max: 2, Score: chkAnchors},
		}},
		{Key: "images", Weight: 0.10, Checks: []Check{
			{Key: "alt_coverage", Max: 5, Score: chkAltCoverage},
			{Key: "image_weight", Max: 3, Score: chkImageWeight},
			{Key: "image_format", Max: 2, Score: chkImageFormat},
		}},
		{Key: "tech", Weight: 0.10, Checks: []Check{
			{Key: "url_clean", Max: 3, Score: chkURLClean},
			{Key: "canonical", Max: 3, Score: chkCanonical},
			{Key: "schema_present", Max: 2, Score: chkSchema},
			{Key: "https", Max: 2, Score: chkHTTPS},
		}},
	}
}
