package main

import (
	"encoding/json"
	"testing"
)

func TestBuildLineageMetadata(t *testing.T) {
	t.Parallel()

	t.Run("NilWhenNoOrigin", func(t *testing.T) {
		result := buildLineageMetadata("", "", "test-actor")
		if result != nil {
			t.Fatalf("expected nil metadata when no origin, got %s", string(result))
		}
	})

	t.Run("HumanOrigin", func(t *testing.T) {
		result := buildLineageMetadata("human", "stiwi", "aegis/crew/goldblum")
		if result == nil {
			t.Fatal("expected non-nil metadata")
		}

		var parsed map[string]any
		if err := json.Unmarshal(result, &parsed); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}

		lineage, ok := parsed["lineage"].(map[string]any)
		if !ok {
			t.Fatal("expected lineage key in metadata")
		}

		if lineage["origin"] != "human" {
			t.Errorf("expected origin=human, got %v", lineage["origin"])
		}
		if lineage["origin_human"] != "stiwi" {
			t.Errorf("expected origin_human=stiwi, got %v", lineage["origin_human"])
		}
		if lineage["executed_by"] != "aegis/crew/goldblum" {
			t.Errorf("expected executed_by=aegis/crew/goldblum, got %v", lineage["executed_by"])
		}
		if _, ok := lineage["origin_timestamp"]; !ok {
			t.Error("expected origin_timestamp to be set")
		}
	})

	t.Run("AgentPatrolNoHuman", func(t *testing.T) {
		result := buildLineageMetadata("agent-patrol", "", "aegis/crew/sentinel")
		if result == nil {
			t.Fatal("expected non-nil metadata")
		}

		var parsed map[string]any
		if err := json.Unmarshal(result, &parsed); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}

		lineage := parsed["lineage"].(map[string]any)
		if lineage["origin"] != "agent-patrol" {
			t.Errorf("expected origin=agent-patrol, got %v", lineage["origin"])
		}
		if _, ok := lineage["origin_human"]; ok {
			t.Error("expected no origin_human for agent-patrol")
		}
	})

	t.Run("SystemOrigin", func(t *testing.T) {
		result := buildLineageMetadata("system", "", "")
		if result == nil {
			t.Fatal("expected non-nil metadata")
		}

		var parsed map[string]any
		if err := json.Unmarshal(result, &parsed); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}

		lineage := parsed["lineage"].(map[string]any)
		if lineage["origin"] != "system" {
			t.Errorf("expected origin=system, got %v", lineage["origin"])
		}
		if _, ok := lineage["executed_by"]; ok {
			t.Error("expected no executed_by when actor is empty")
		}
	})
}
