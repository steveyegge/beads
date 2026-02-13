package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/steveyegge/beads/internal/linear"
	"github.com/steveyegge/beads/internal/types"
)

const (
	SourceTypeLinear = "linear"
)

// LinearSource discovers labels from Linear and exposes them as skill resources
type LinearSource struct {
	Client *linear.Client
}

// NewLinearSource creates a new LinearSource
func NewLinearSource(apiKey, teamID string) *LinearSource {
	if apiKey == "" {
		apiKey = os.Getenv("LINEAR_API_KEY")
	}
	if apiKey == "" {
		return &LinearSource{Client: nil}
	}
	return &LinearSource{Client: linear.NewClient(apiKey, teamID)}
}

// Name returns the source name
func (l *LinearSource) Name() string {
	return types.ResourceSourceLinear
}

// Discover fetches labels from Linear
func (l *LinearSource) Discover(ctx context.Context) ([]*types.Resource, error) {
	if l.Client == nil {
		fmt.Fprintf(os.Stderr, "Warning: LINEAR_API_KEY not set, skipping Linear discovery\n")
		return []*types.Resource{}, nil
	}

	labels, err := l.fetchLabels(ctx)
	if err != nil {
		return nil, err
	}

	var resources []*types.Resource
	for _, label := range labels {
		configMap := map[string]interface{}{
			"color":       label.Color,
			"description": label.Description,
			"id":          label.ID,
		}
		configJSON, _ := json.Marshal(configMap)

		resources = append(resources, &types.Resource{
			Name:       label.Name,
			Type:       types.ResourceTypeSkill,
			Identifier: fmt.Sprintf("linear-label-%s", label.Name),
			Source:     types.ResourceSourceLinear,
			ExternalID: label.ID,
			Config:     string(configJSON),
			IsActive:   true,
			Tags:       []string{"linear", "label"},
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		})
	}

	return resources, nil
}

type linearLabel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

func (l *LinearSource) fetchLabels(ctx context.Context) ([]linearLabel, error) {
	query := `query {
		issueLabels {
			nodes {
				id
				name
				color
				description
			}
		}
	}`

	req := &linear.GraphQLRequest{Query: query}
	result, err := l.Client.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	var response struct {
		Data struct {
			Labels struct {
				Nodes []linearLabel `json:"nodes"`
			} `json:"issueLabels"`
		}
	}

	if err := json.Unmarshal(result, &response); err != nil {
		return nil, err
	}

	return response.Data.Labels.Nodes, nil
}
