package pubservice

type AuditResult struct {
	Scanned int          `json:"scanned"`
	Count   int          `json:"count"`
	Issues  []AuditIssue `json:"issues"`
}

type AuditIssue struct {
	Path   string           `json:"path"`
	Item   string           `json:"item"`
	Level  string           `json:"level"`
	Params AuditIssueParams `json:"params"`
}

type AuditIssueParams struct {
	Title string   `json:"title,omitempty"`
	Src   string   `json:"src,omitempty"`
	Href  string   `json:"href,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

func newAuditResult(scanned int, issues []AuditIssue) AuditResult {
	if issues == nil {
		issues = []AuditIssue{}
	}
	return AuditResult{Scanned: scanned, Count: len(issues), Issues: issues}
}
