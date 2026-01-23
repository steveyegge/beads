// Package webhook provides HTTP endpoints for receiving decision responses.
// This is the "Response Webhook" described in the Decision Points design doc.
//
// hq-946577.22: Response webhook endpoint handler
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// TokenClaims contains the claims encoded in a response token.
type TokenClaims struct {
	DecisionID string    `json:"decision_id"` // The decision point this token is for
	Expiry     time.Time `json:"exp"`         // When the token expires
	Respondent string    `json:"respondent"`  // Expected respondent (optional)
}

// GenerateResponseToken creates an HMAC-signed token for a decision response URL.
// The token encodes the decision ID, expiry time, and optional expected respondent.
//
// Token format: base64(json(claims)).base64(hmac-sha256(claims))
func GenerateResponseToken(decisionID string, expiry time.Time, respondent string, secret []byte) (string, error) {
	claims := TokenClaims{
		DecisionID: decisionID,
		Expiry:     expiry,
		Respondent: respondent,
	}

	// Encode claims as JSON
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token claims: %w", err)
	}

	// Create HMAC signature
	h := hmac.New(sha256.New, secret)
	h.Write(claimsJSON)
	signature := h.Sum(nil)

	// Encode as base64
	claimsB64 := base64.URLEncoding.EncodeToString(claimsJSON)
	sigB64 := base64.URLEncoding.EncodeToString(signature)

	return claimsB64 + "." + sigB64, nil
}

// ValidateResponseToken validates an HMAC-signed response token.
// Returns the decoded claims if valid, or an error if invalid.
func ValidateResponseToken(token string, secret []byte) (*TokenClaims, error) {
	// Split token into claims and signature
	var claimsB64, sigB64 string
	for i := len(token) - 1; i >= 0; i-- {
		if token[i] == '.' {
			claimsB64 = token[:i]
			sigB64 = token[i+1:]
			break
		}
	}
	if claimsB64 == "" || sigB64 == "" {
		return nil, fmt.Errorf("invalid token format")
	}

	// Decode claims
	claimsJSON, err := base64.URLEncoding.DecodeString(claimsB64)
	if err != nil {
		return nil, fmt.Errorf("invalid token encoding: %w", err)
	}

	// Decode signature
	signature, err := base64.URLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("invalid signature encoding: %w", err)
	}

	// Verify HMAC signature
	h := hmac.New(sha256.New, secret)
	h.Write(claimsJSON)
	expectedSig := h.Sum(nil)
	if !hmac.Equal(signature, expectedSig) {
		return nil, fmt.Errorf("invalid token signature")
	}

	// Parse claims
	var claims TokenClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("invalid token claims: %w", err)
	}

	// Check expiry
	if time.Now().After(claims.Expiry) {
		return nil, fmt.Errorf("token expired at %s", claims.Expiry.Format(time.RFC3339))
	}

	return &claims, nil
}

// ValidateRespondent checks if the actual respondent matches the expected respondent.
// If expectedRespondent is empty, any respondent is allowed.
// If strictMode is false, returns true with a warning message instead of error.
func ValidateRespondent(expected, actual string, strictMode bool) (bool, string) {
	if expected == "" {
		// No respondent restriction
		return true, ""
	}
	if expected == actual {
		return true, ""
	}
	if !strictMode {
		return true, fmt.Sprintf("respondent mismatch: expected %q, got %q (allowed in non-strict mode)", expected, actual)
	}
	return false, fmt.Sprintf("respondent mismatch: expected %q, got %q", expected, actual)
}
