package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const evidenceTuplePrefix = "EvidenceTuple: "

type evidenceTuple struct {
	TS         string `json:"ts"`
	EnvID      string `json:"env_id"`
	ArtifactID string `json:"artifact_id"`
}

type parsedEvidenceTuple struct {
	Raw       evidenceTuple
	Timestamp time.Time
}

func canonicalEvidenceTupleLine(tuple evidenceTuple) (string, error) {
	parsedTS, err := parseEvidenceTimestamp(tuple.TS)
	if err != nil {
		return "", err
	}
	tuple.TS = parsedTS.Format(time.RFC3339)
	tuple.EnvID = strings.TrimSpace(tuple.EnvID)
	tuple.ArtifactID = strings.TrimSpace(tuple.ArtifactID)
	if tuple.EnvID == "" {
		return "", fmt.Errorf("env_id is required")
	}
	if tuple.ArtifactID == "" {
		return "", fmt.Errorf("artifact_id is required")
	}
	encoded, err := json.Marshal(tuple)
	if err != nil {
		return "", fmt.Errorf("failed to encode evidence tuple: %w", err)
	}
	return evidenceTuplePrefix + string(encoded), nil
}

func parseEvidenceTimestamp(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("ts is required")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("ts must be RFC3339: %w", err)
	}
	return parsed.UTC(), nil
}

func parseEvidenceTupleNotes(notes string) ([]parsedEvidenceTuple, error) {
	lines := strings.Split(notes, "\n")
	parsedTuples := make([]parsedEvidenceTuple, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, evidenceTuplePrefix) {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, evidenceTuplePrefix))
		if payload == "" {
			return nil, fmt.Errorf("evidence tuple payload is empty")
		}

		var tuple evidenceTuple
		if err := json.Unmarshal([]byte(payload), &tuple); err != nil {
			return nil, fmt.Errorf("invalid evidence tuple JSON: %w", err)
		}

		lineValue, err := canonicalEvidenceTupleLine(tuple)
		if err != nil {
			return nil, err
		}

		var normalized evidenceTuple
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lineValue, evidenceTuplePrefix)), &normalized); err != nil {
			return nil, fmt.Errorf("failed to normalize evidence tuple: %w", err)
		}

		ts, err := parseEvidenceTimestamp(normalized.TS)
		if err != nil {
			return nil, err
		}

		parsedTuples = append(parsedTuples, parsedEvidenceTuple{
			Raw:       normalized,
			Timestamp: ts,
		})
	}
	return parsedTuples, nil
}

func validateEvidenceTupleNotes(notes string, now time.Time, maxAge time.Duration) error {
	if maxAge <= 0 {
		return fmt.Errorf("evidence max age must be positive")
	}
	parsedTuples, err := parseEvidenceTupleNotes(notes)
	if err != nil {
		return err
	}
	if len(parsedTuples) == 0 {
		return fmt.Errorf("missing evidence tuple")
	}

	cutoff := now.UTC().Add(-maxAge)
	freshest := parsedTuples[0].Timestamp
	for _, tuple := range parsedTuples[1:] {
		if tuple.Timestamp.After(freshest) {
			freshest = tuple.Timestamp
		}
	}
	if freshest.Before(cutoff) {
		return fmt.Errorf("evidence tuple is stale (latest=%s, max_age=%s)", freshest.Format(time.RFC3339), maxAge.String())
	}
	return nil
}
