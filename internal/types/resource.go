package types

import "time"

// ResourceType constants
const (
	ResourceTypeModel = "model"
	ResourceTypeAgent = "agent"
	ResourceTypeSkill = "skill"
)

// ResourceSource constants
const (
	ResourceSourceLocal  = "local"
	ResourceSourceLinear = "linear"
	ResourceSourceJira   = "jira"
	ResourceSourceConfig = "config"
)

// Resource represents a capability (model, agent, skill)
type Resource struct {
	ID         int64     `json:"id"`
	Type       string    `json:"type"`       // 'model', 'agent', 'skill'
	Name       string    `json:"name"`       // Display Name
	Identifier string    `json:"identifier"` // System ID (unique)
	Source     string    `json:"source"`     // 'local', 'linear', 'jira', 'config'
	ExternalID string    `json:"external_id,omitempty"`
	Config     string    `json:"config_json,omitempty"`
	IsActive   bool      `json:"is_active"`
	Tags       []string  `json:"tags,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ResourceFilter is used to query resources
type ResourceFilter struct {
	Type   *string
	Source *string
	Tags   []string // AND semantics
}
