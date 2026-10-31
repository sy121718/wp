package plugindto

import json "go_wp/pkg/jsonx"

type PluginResp struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Version        string          `json:"version"`
	SchemaVersion  int             `json:"schemaVersion"`
	Enabled        bool            `json:"enabled"`
	ComponentCount int             `json:"componentCount"`
	InstalledAt    string          `json:"installedAt"`
	UpdatedAt      string          `json:"updatedAt"`
	Manifest       json.RawMessage `json:"manifest,omitempty"`
}

type SchemaInfo struct {
	Name       string `json:"name"`
	TableCount int    `json:"tableCount"`
}

type PatrolResp struct {
	OrphanSchemas  []SchemaInfo `json:"orphanSchemas"`
	MissingSchemas []string     `json:"missingSchemas"`
	OrphanStorage  []string     `json:"orphanStorage"`
	MissingStorage []string     `json:"missingStorage"`

	StorageRoot string `json:"storageRoot"`
}

type ComponentSummary struct {
	Type  string         `json:"type"`
	Label string         `json:"label"`
	Hint  string         `json:"hint"`
	Props map[string]any `json:"props"`
}

type PresetSummary struct {
	ID        string          `json:"id"`
	Label     string          `json:"label"`
	Category  string          `json:"category,omitempty"`
	Thumbnail string          `json:"thumbnail,omitempty"`
	Document  json.RawMessage `json:"document"`
}
