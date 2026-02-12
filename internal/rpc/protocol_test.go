package rpc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestSerialization(t *testing.T) {
	createArgs := CreateArgs{
		Title:       "Test Issue",
		Description: "Test description",
		IssueType:   "task",
		Priority:    2,
	}

	argsJSON, err := json.Marshal(createArgs)
	if err != nil {
		t.Fatalf("Failed to marshal args: %v", err)
	}

	req := Request{
		Operation: OpCreate,
		Args:      argsJSON,
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	var decodedReq Request
	if err := json.Unmarshal(reqJSON, &decodedReq); err != nil {
		t.Fatalf("Failed to unmarshal request: %v", err)
	}

	if decodedReq.Operation != OpCreate {
		t.Errorf("Expected operation %s, got %s", OpCreate, decodedReq.Operation)
	}

	var decodedArgs CreateArgs
	if err := json.Unmarshal(decodedReq.Args, &decodedArgs); err != nil {
		t.Fatalf("Failed to unmarshal args: %v", err)
	}

	if decodedArgs.Title != createArgs.Title {
		t.Errorf("Expected title %s, got %s", createArgs.Title, decodedArgs.Title)
	}
	if decodedArgs.Priority != createArgs.Priority {
		t.Errorf("Expected priority %d, got %d", createArgs.Priority, decodedArgs.Priority)
	}
}

func TestResponseSerialization(t *testing.T) {
	resp := Response{
		Success: true,
		Data:    json.RawMessage(`{"id":"bd-1","title":"Test"}`),
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var decodedResp Response
	if err := json.Unmarshal(respJSON, &decodedResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if decodedResp.Success != resp.Success {
		t.Errorf("Expected success %v, got %v", resp.Success, decodedResp.Success)
	}

	if string(decodedResp.Data) != string(resp.Data) {
		t.Errorf("Expected data %s, got %s", string(resp.Data), string(decodedResp.Data))
	}
}

func TestErrorResponse(t *testing.T) {
	resp := Response{
		Success: false,
		Error:   "something went wrong",
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal response: %v", err)
	}

	var decodedResp Response
	if err := json.Unmarshal(respJSON, &decodedResp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if decodedResp.Success {
		t.Errorf("Expected success false, got true")
	}

	if decodedResp.Error != resp.Error {
		t.Errorf("Expected error %s, got %s", resp.Error, decodedResp.Error)
	}
}

func TestAllOperations(t *testing.T) {
	operations := []string{
		OpPing,
		OpCreate,
		OpUpdate,
		OpClose,
		OpList,
		OpShow,
		OpReady,
		OpStats,
		OpDepAdd,
		OpDepRemove,
		OpDepTree,
		OpLabelAdd,
		OpLabelRemove,
		OpCommentList,
		OpCommentAdd,
	}

	for _, op := range operations {
		req := Request{
			Operation: op,
			Args:      json.RawMessage(`{}`),
		}

		reqJSON, err := json.Marshal(req)
		if err != nil {
			t.Errorf("Failed to marshal request for op %s: %v", op, err)
			continue
		}

		var decodedReq Request
		if err := json.Unmarshal(reqJSON, &decodedReq); err != nil {
			t.Errorf("Failed to unmarshal request for op %s: %v", op, err)
			continue
		}

		if decodedReq.Operation != op {
			t.Errorf("Expected operation %s, got %s", op, decodedReq.Operation)
		}
	}
}

func TestValidateSidecarMetadata(t *testing.T) {
	tests := []struct {
		name    string
		meta    map[string]string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty metadata",
			meta:    map[string]string{},
			wantErr: false,
		},
		{
			name:    "non-sidecar keys only",
			meta:    map[string]string{"foo": "bar", "execution_target": "k8s"},
			wantErr: false,
		},
		{
			name:    "valid profile toolchain-full",
			meta:    map[string]string{MetaSidecarProfile: "toolchain-full"},
			wantErr: false,
		},
		{
			name:    "valid profile toolchain-minimal",
			meta:    map[string]string{MetaSidecarProfile: "toolchain-minimal"},
			wantErr: false,
		},
		{
			name:    "valid profile none",
			meta:    map[string]string{MetaSidecarProfile: "none"},
			wantErr: false,
		},
		{
			name:    "invalid profile",
			meta:    map[string]string{MetaSidecarProfile: "custom-bogus"},
			wantErr: true,
			errMsg:  "invalid sidecar_profile",
		},
		{
			name:    "valid image with registry",
			meta:    map[string]string{MetaSidecarImage: "ghcr.io/groblegark/toolchain:latest"},
			wantErr: false,
		},
		{
			name:    "invalid image empty",
			meta:    map[string]string{MetaSidecarImage: ""},
			wantErr: true,
			errMsg:  "sidecar_image cannot be empty",
		},
		{
			name:    "invalid image no registry prefix",
			meta:    map[string]string{MetaSidecarImage: "toolchain:latest"},
			wantErr: true,
			errMsg:  "must include a registry prefix",
		},
		{
			name:    "valid CPU millicores",
			meta:    map[string]string{MetaSidecarResourcesCPU: "500m"},
			wantErr: false,
		},
		{
			name:    "valid CPU whole cores",
			meta:    map[string]string{MetaSidecarResourcesCPU: "2"},
			wantErr: false,
		},
		{
			name:    "invalid CPU",
			meta:    map[string]string{MetaSidecarResourcesCPU: "lots"},
			wantErr: true,
			errMsg:  "invalid sidecar_resources_cpu",
		},
		{
			name:    "valid memory Mi",
			meta:    map[string]string{MetaSidecarResourcesMemory: "512Mi"},
			wantErr: false,
		},
		{
			name:    "valid memory Gi",
			meta:    map[string]string{MetaSidecarResourcesMemory: "2Gi"},
			wantErr: false,
		},
		{
			name:    "invalid memory",
			meta:    map[string]string{MetaSidecarResourcesMemory: "a-lot"},
			wantErr: true,
			errMsg:  "invalid sidecar_resources_memory",
		},
		{
			name: "mixed valid sidecar and non-sidecar keys",
			meta: map[string]string{
				"execution_target":         "k8s",
				MetaSidecarProfile:         "toolchain-full",
				MetaSidecarResourcesCPU:    "1",
				MetaSidecarResourcesMemory: "1Gi",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSidecarMetadata(tt.meta)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errMsg)
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestValidateSidecarMetadataJSON(t *testing.T) {
	tests := []struct {
		name    string
		raw     json.RawMessage
		wantErr bool
	}{
		{
			name:    "nil metadata",
			raw:     nil,
			wantErr: false,
		},
		{
			name:    "empty metadata",
			raw:     json.RawMessage(`{}`),
			wantErr: false,
		},
		{
			name:    "non-sidecar keys only",
			raw:     json.RawMessage(`{"foo":"bar"}`),
			wantErr: false,
		},
		{
			name:    "valid sidecar profile",
			raw:     json.RawMessage(`{"sidecar_profile":"toolchain-full"}`),
			wantErr: false,
		},
		{
			name:    "invalid sidecar profile",
			raw:     json.RawMessage(`{"sidecar_profile":"bad"}`),
			wantErr: true,
		},
		{
			name:    "non-string-map metadata skipped",
			raw:     json.RawMessage(`{"sidecar_profile": 123}`),
			wantErr: false, // can't unmarshal to map[string]string, so validation is skipped
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSidecarMetadataJSON(tt.raw)
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestUpdateArgsWithNilValues(t *testing.T) {
	title := "New Title"
	args := UpdateArgs{
		ID:    "bd-1",
		Title: &title,
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("Failed to marshal args: %v", err)
	}

	var decodedArgs UpdateArgs
	if err := json.Unmarshal(argsJSON, &decodedArgs); err != nil {
		t.Fatalf("Failed to unmarshal args: %v", err)
	}

	if decodedArgs.Title == nil {
		t.Errorf("Expected title to be non-nil")
	} else if *decodedArgs.Title != title {
		t.Errorf("Expected title %s, got %s", title, *decodedArgs.Title)
	}

	if decodedArgs.Status != nil {
		t.Errorf("Expected status to be nil, got %v", *decodedArgs.Status)
	}
}
