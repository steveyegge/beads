package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

const (
	SourceTypeLinear = "linear"
	linearGraphQLEndpoint = "https://api.linear.app/graphql"
)

// LinearSource discovers labels from Linear and exposes them as skill resources
type LinearSource struct {
	APIKey string
}

// NewLinearSource creates a new LinearSource
func NewLinearSource() *LinearSource {
	apiKey := os.Getenv("LINEAR_API_KEY")
	return &LinearSource{APIKey: apiKey}
}

// Name returns the source name
func (l *LinearSource) Name() string {
	return types.ResourceSourceLinear
}

// Discover fetches labels from Linear
func (l *LinearSource) Discover(ctx context.Context) ([]*types.Resource, error) {
	if l.APIKey == "" {
		// Log warning but don't fail, just return empty
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

type linearResponse struct {
	Data struct {
		WorkflowStates struct {
			Nodes []linearLabel `json:"nodes"`
		} `json:"workflowStates"`
		Labels struct {
			Nodes []linearLabel `json:"nodes"`
		} `json:"issueLabels"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
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

	reqBody := map[string]string{"query": query}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", linearGraphQLEndpoint, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", l.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("linear api returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result linearResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("linear api error: %s", result.Errors[0].Message)
	}

	return result.Data.Labels.Nodes, nil
}
