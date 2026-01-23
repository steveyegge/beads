package webhook

import (
	"testing"
	"time"
)

func TestGenerateAndValidateToken(t *testing.T) {
	secret := []byte("test-secret-key")
	decisionID := "gt-abc123.decision-1"
	expiry := time.Now().Add(24 * time.Hour)
	respondent := "user@example.com"

	// Generate token
	token, err := GenerateResponseToken(decisionID, expiry, respondent, secret)
	if err != nil {
		t.Fatalf("GenerateResponseToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	// Validate token
	claims, err := ValidateResponseToken(token, secret)
	if err != nil {
		t.Fatalf("ValidateResponseToken failed: %v", err)
	}

	// Check claims
	if claims.DecisionID != decisionID {
		t.Errorf("DecisionID = %q, want %q", claims.DecisionID, decisionID)
	}
	if claims.Respondent != respondent {
		t.Errorf("Respondent = %q, want %q", claims.Respondent, respondent)
	}
	// Expiry should be close (within a second due to encoding)
	if claims.Expiry.Sub(expiry).Abs() > time.Second {
		t.Errorf("Expiry = %v, want close to %v", claims.Expiry, expiry)
	}
}

func TestValidateToken_WrongSecret(t *testing.T) {
	secret := []byte("test-secret-key")
	wrongSecret := []byte("wrong-secret-key")
	decisionID := "gt-abc123.decision-1"
	expiry := time.Now().Add(24 * time.Hour)

	// Generate with correct secret
	token, err := GenerateResponseToken(decisionID, expiry, "", secret)
	if err != nil {
		t.Fatalf("GenerateResponseToken failed: %v", err)
	}

	// Validate with wrong secret
	_, err = ValidateResponseToken(token, wrongSecret)
	if err == nil {
		t.Fatal("expected error validating with wrong secret")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	secret := []byte("test-secret-key")
	decisionID := "gt-abc123.decision-1"
	expiry := time.Now().Add(-1 * time.Hour) // Already expired

	// Generate expired token
	token, err := GenerateResponseToken(decisionID, expiry, "", secret)
	if err != nil {
		t.Fatalf("GenerateResponseToken failed: %v", err)
	}

	// Validate should fail
	_, err = ValidateResponseToken(token, secret)
	if err == nil {
		t.Fatal("expected error validating expired token")
	}
}

func TestValidateToken_InvalidFormat(t *testing.T) {
	secret := []byte("test-secret-key")

	testCases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no dot", "abcdef123456"},
		{"empty claims", ".abc123"},
		{"empty signature", "abc123."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateResponseToken(tc.token, secret)
			if err == nil {
				t.Error("expected error for invalid token format")
			}
		})
	}
}

func TestValidateRespondent(t *testing.T) {
	testCases := []struct {
		name       string
		expected   string
		actual     string
		strictMode bool
		wantOK     bool
		wantMsg    bool
	}{
		{"empty expected allows any", "", "anyone@example.com", true, true, false},
		{"matching respondent", "user@example.com", "user@example.com", true, true, false},
		{"mismatched strict", "user@example.com", "other@example.com", true, false, true},
		{"mismatched non-strict", "user@example.com", "other@example.com", false, true, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ok, msg := ValidateRespondent(tc.expected, tc.actual, tc.strictMode)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v", ok, tc.wantOK)
			}
			hasMsg := msg != ""
			if hasMsg != tc.wantMsg {
				t.Errorf("msg presence = %v, want %v", hasMsg, tc.wantMsg)
			}
		})
	}
}
