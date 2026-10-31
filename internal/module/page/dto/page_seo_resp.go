package pagedto

import "go_wp/pkg/utils"

type SEOFindingItemResp struct {
	URL string `json:"url"`

	Lang string `json:"lang,omitempty"`

	ArtifactHash string `json:"artifactHash,omitempty"`

	Rule string `json:"rule"`

	Level string `json:"level"`

	Evidence string `json:"evidence"`

	Expect string `json:"expect"`
}

type SEOArtifactItemResp struct {
	URL          string `json:"url"`
	Lang         string `json:"lang,omitempty"`
	ArtifactHash string `json:"artifactHash,omitempty"`

	Checks int `json:"checks"`

	Index          bool   `json:"index"`
	Follow         bool   `json:"follow"`
	RobotsExplicit bool   `json:"robotsExplicit"`
	Robots         string `json:"robots,omitempty"`

	Findings []SEOFindingItemResp `json:"findings"`
}

type SEOUncheckedRouteResp struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

type SEOPatrolResp struct {
	ProjectID string `json:"projectId"`

	SampledAt utils.JSONTime `json:"sampledAt"`

	Checked int `json:"checked"`
	Healthy int `json:"healthy"`

	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`

	ChecksPerArtifact int `json:"checksPerArtifact"`

	Artifacts []SEOArtifactItemResp `json:"artifacts"`

	CrossFindings []SEOFindingItemResp `json:"crossFindings"`

	Unchecked []SEOUncheckedRouteResp `json:"unchecked"`
}
