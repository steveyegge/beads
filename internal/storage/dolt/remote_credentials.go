package dolt

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
)

// AddRemoteCredentials adds or updates credentials for a Dolt remote.
// The password is encrypted at rest using AES-256-GCM.
func (s *DoltStore) AddRemoteCredentials(ctx context.Context, remoteName, username, password string) error {
	if remoteName == "" {
		return fmt.Errorf("remote name cannot be empty")
	}
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	// Encrypt password before storing
	var encryptedPwd []byte
	var err error
	if password != "" {
		encryptedPwd, err = s.encryptPassword(password)
		if err != nil {
			return fmt.Errorf("failed to encrypt password: %w", err)
		}
	}

	// Upsert the remote credentials
	_, err = s.execContext(ctx, `
		INSERT INTO remote_credentials (remote_name, username, password_encrypted)
		VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE
			username = VALUES(username),
			password_encrypted = VALUES(password_encrypted),
			updated_at = CURRENT_TIMESTAMP
	`, remoteName, username, encryptedPwd)

	if err != nil {
		return fmt.Errorf("failed to add remote credentials: %w", err)
	}

	return nil
}

// GetRemoteCredentials retrieves credentials for a Dolt remote by name.
// Returns storage.ErrNotFound (wrapped) if no credentials exist for the remote.
func (s *DoltStore) GetRemoteCredentials(ctx context.Context, remoteName string) (username, password string, err error) {
	var encryptedPwd []byte

	err = s.db.QueryRowContext(ctx, `
		SELECT username, password_encrypted
		FROM remote_credentials WHERE remote_name = ?
	`, remoteName).Scan(&username, &encryptedPwd)

	if err == sql.ErrNoRows {
		return "", "", fmt.Errorf("%w: remote credentials for %s", storage.ErrNotFound, remoteName)
	}
	if err != nil {
		return "", "", fmt.Errorf("failed to get remote credentials: %w", err)
	}

	// Decrypt password
	if len(encryptedPwd) > 0 {
		password, err = s.decryptPassword(encryptedPwd)
		if err != nil {
			return "", "", fmt.Errorf("failed to decrypt password: %w", err)
		}
	}

	return username, password, nil
}

// RemoveRemoteCredentials removes credentials for a Dolt remote.
func (s *DoltStore) RemoveRemoteCredentials(ctx context.Context, remoteName string) error {
	_, err := s.execContext(ctx, "DELETE FROM remote_credentials WHERE remote_name = ?", remoteName)
	if err != nil {
		return fmt.Errorf("failed to remove remote credentials: %w", err)
	}
	return nil
}
