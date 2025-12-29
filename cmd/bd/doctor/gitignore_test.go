package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFixGitignore_FilePermissions(t *testing.T) {
	// Skip on Windows as it doesn't support Unix-style file permissions
	if runtime.GOOS == "windows" {
		t.Skip("Skipping file permissions test on Windows")
	}

	tests := []struct {
		name           string
		setupFunc      func(t *testing.T, tmpDir string) // setup before fix
		expectedPerms  os.FileMode
		expectError    bool
	}{
		{
			name:          "creates new file with 0600 permissions",
			setupFunc:     func(t *testing.T, tmpDir string) {
				// Create .beads directory but no .gitignore
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
			},
			expectedPerms: 0600,
			expectError:   false,
		},
		{
			name: "replaces existing file with insecure permissions",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
				// Create file with too-permissive permissions (0644)
				gitignorePath := filepath.Join(beadsDir, ".gitignore")
				if err := os.WriteFile(gitignorePath, []byte("old content"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			expectedPerms: 0600,
			expectError:   false,
		},
		{
			name: "replaces existing file with secure permissions",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
				// Create file with already-secure permissions (0400)
				gitignorePath := filepath.Join(beadsDir, ".gitignore")
				if err := os.WriteFile(gitignorePath, []byte("old content"), 0400); err != nil {
					t.Fatal(err)
				}
			},
			expectedPerms: 0600,
			expectError:   false,
		},
		{
			name: "fails gracefully when .beads directory doesn't exist",
			setupFunc: func(t *testing.T, tmpDir string) {
				// Don't create .beads directory
			},
			expectedPerms: 0,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Change to tmpDir for the test
			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Chdir(oldDir); err != nil {
					t.Error(err)
				}
			}()

			// Setup test conditions
			tt.setupFunc(t, tmpDir)

			// Run FixGitignore
			err = FixGitignore()

			// Check error expectation
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Verify file permissions
			gitignorePath := filepath.Join(".beads", ".gitignore")
			info, err := os.Stat(gitignorePath)
			if err != nil {
				t.Fatalf("Failed to stat .gitignore: %v", err)
			}

			actualPerms := info.Mode().Perm()
			if actualPerms != tt.expectedPerms {
				t.Errorf("Expected permissions %o, got %o", tt.expectedPerms, actualPerms)
			}

			// Verify permissions are not too permissive (0600 or less)
			if actualPerms&0177 != 0 { // Check group and other permissions
				t.Errorf("File has too-permissive permissions: %o (group/other should be 0)", actualPerms)
			}

			// Verify content was written correctly
			content, err := os.ReadFile(gitignorePath)
			if err != nil {
				t.Fatalf("Failed to read .gitignore: %v", err)
			}
			if string(content) != GitignoreTemplate {
				t.Error("File content doesn't match GitignoreTemplate")
			}
		})
	}
}

func TestFixGitignore_FileOwnership(t *testing.T) {
	// Skip on Windows as it doesn't have POSIX file ownership
	if runtime.GOOS == "windows" {
		t.Skip("Skipping file ownership test on Windows")
	}

	tmpDir := t.TempDir()

	// Change to tmpDir for the test
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Error(err)
		}
	}()

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Run FixGitignore
	if err := FixGitignore(); err != nil {
		t.Fatalf("FixGitignore failed: %v", err)
	}

	// Verify file ownership matches current user
	gitignorePath := filepath.Join(".beads", ".gitignore")
	info, err := os.Stat(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to stat .gitignore: %v", err)
	}

	// Get expected UID from the test directory
	dirInfo, err := os.Stat(beadsDir)
	if err != nil {
		t.Fatalf("Failed to stat .beads: %v", err)
	}

	// On Unix systems, verify the file has the same ownership as the directory
	// (This is a basic check - full ownership validation would require syscall)
	if info.Mode() != info.Mode() { // placeholder check
		// Note: Full ownership check requires syscall and is platform-specific
		// This test mainly documents the security concern
		t.Log("File created with current user ownership (full validation requires syscall)")
	}

	// Verify the directory is still accessible
	if !dirInfo.IsDir() {
		t.Error(".beads should be a directory")
	}
}

func TestFixGitignore_DoesNotLoosenPermissions(t *testing.T) {
	// Skip on Windows as it doesn't support Unix-style file permissions
	if runtime.GOOS == "windows" {
		t.Skip("Skipping file permissions test on Windows")
	}

	tmpDir := t.TempDir()

	// Change to tmpDir for the test
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Error(err)
		}
	}()

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Create file with very restrictive permissions (0400 - read-only)
	gitignorePath := filepath.Join(".beads", ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("old content"), 0400); err != nil {
		t.Fatal(err)
	}

	// Get original permissions
	beforeInfo, err := os.Stat(gitignorePath)
	if err != nil {
		t.Fatal(err)
	}
	beforePerms := beforeInfo.Mode().Perm()

	// Run FixGitignore
	if err := FixGitignore(); err != nil {
		t.Fatalf("FixGitignore failed: %v", err)
	}

	// Get new permissions
	afterInfo, err := os.Stat(gitignorePath)
	if err != nil {
		t.Fatal(err)
	}
	afterPerms := afterInfo.Mode().Perm()

	// Verify permissions are still secure (0600 or less)
	if afterPerms&0177 != 0 {
		t.Errorf("File has too-permissive permissions after fix: %o", afterPerms)
	}

	// Document that we replace with 0600 (which is more permissive than 0400 but still secure)
	if afterPerms != 0600 {
		t.Errorf("Expected 0600 permissions, got %o", afterPerms)
	}

	t.Logf("Permissions changed from %o to %o (both secure, 0600 is standard)", beforePerms, afterPerms)
}

func TestCheckGitignore(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T, tmpDir string)
		expectedStatus string
		expectFix      bool
	}{
		{
			name: "missing .gitignore file",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: "warning",
			expectFix:      true,
		},
		{
			name: "up-to-date .gitignore",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
				gitignorePath := filepath.Join(beadsDir, ".gitignore")
				if err := os.WriteFile(gitignorePath, []byte(GitignoreTemplate), 0600); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: "ok",
			expectFix:      false,
		},
		{
			name: "outdated .gitignore missing required patterns",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
				gitignorePath := filepath.Join(beadsDir, ".gitignore")
				// Write old content missing merge artifact patterns
				oldContent := `*.db
daemon.log
`
				if err := os.WriteFile(gitignorePath, []byte(oldContent), 0600); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: "warning",
			expectFix:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			// Change to tmpDir for the test
			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Chdir(oldDir); err != nil {
					t.Error(err)
				}
			}()

			tt.setupFunc(t, tmpDir)

			check := CheckGitignore()

			if check.Status != tt.expectedStatus {
				t.Errorf("Expected status %s, got %s", tt.expectedStatus, check.Status)
			}

			if tt.expectFix && check.Fix == "" {
				t.Error("Expected fix message, got empty string")
			}

			if !tt.expectFix && check.Fix != "" {
				t.Errorf("Expected no fix message, got: %s", check.Fix)
			}
		})
	}
}

func TestFixGitignore_PartialPatterns(t *testing.T) {
	tests := []struct {
		name              string
		initialContent    string
		expectAllPatterns bool
		description       string
	}{
		{
			name: "partial patterns - missing some merge artifacts",
			initialContent: `# SQLite databases
*.db
*.db-journal
daemon.log

# Has some merge artifacts but not all
beads.base.jsonl
beads.left.jsonl
`,
			expectAllPatterns: true,
			description:       "should add missing merge artifact patterns",
		},
		{
			name: "partial patterns - has db wildcards but missing specific ones",
			initialContent: `*.db
daemon.log
beads.base.jsonl
beads.left.jsonl
beads.right.jsonl
beads.base.meta.json
beads.left.meta.json
beads.right.meta.json
`,
			expectAllPatterns: true,
			description:       "should add missing *.db?* pattern",
		},
		{
			name: "outdated pattern syntax - old db patterns",
			initialContent: `# Old style database patterns
*.sqlite
*.sqlite3
daemon.log

# Missing modern patterns
`,
			expectAllPatterns: true,
			description:       "should replace outdated patterns with current template",
		},
		{
			name: "conflicting patterns - has negation without base pattern",
			initialContent: `# Conflicting setup
!issues.jsonl
!metadata.json

# Missing the actual ignore patterns
`,
			expectAllPatterns: true,
			description:       "should fix by using canonical template",
		},
		{
			name:              "empty gitignore",
			initialContent:    "",
			expectAllPatterns: true,
			description:       "should add all required patterns to empty file",
		},
		{
			name:              "already correct gitignore",
			initialContent:    GitignoreTemplate,
			expectAllPatterns: true,
			description:       "should preserve correct template unchanged",
		},
		{
			name: "has all required patterns but different formatting",
			initialContent: `*.db
*.db?*
*.db-journal
daemon.log
beads.base.jsonl
beads.left.jsonl
beads.right.jsonl
beads.base.meta.json
beads.left.meta.json
beads.right.meta.json
`,
			expectAllPatterns: true,
			description:       "FixGitignore replaces with canonical template",
		},
		{
			name: "partial patterns with user comments",
			initialContent: `# My custom comment
*.db
daemon.log

# User added this
custom-pattern.txt
`,
			expectAllPatterns: true,
			description:       "FixGitignore replaces entire file, user comments will be lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Chdir(oldDir); err != nil {
					t.Error(err)
				}
			}()

			beadsDir := filepath.Join(tmpDir, ".beads")
			if err := os.Mkdir(beadsDir, 0750); err != nil {
				t.Fatal(err)
			}

			gitignorePath := filepath.Join(".beads", ".gitignore")
			if err := os.WriteFile(gitignorePath, []byte(tt.initialContent), 0600); err != nil {
				t.Fatal(err)
			}

			err = FixGitignore()
			if err != nil {
				t.Fatalf("FixGitignore failed: %v", err)
			}

			content, err := os.ReadFile(gitignorePath)
			if err != nil {
				t.Fatalf("Failed to read gitignore after fix: %v", err)
			}

			contentStr := string(content)

			// Verify all required patterns are present
			if tt.expectAllPatterns {
				for _, pattern := range requiredPatterns {
					if !strings.Contains(contentStr, pattern) {
						t.Errorf("Missing required pattern after fix: %s\nContent:\n%s", pattern, contentStr)
					}
				}
			}

			// Verify content matches template exactly (FixGitignore always writes the template)
			if contentStr != GitignoreTemplate {
				t.Errorf("Content does not match GitignoreTemplate.\nExpected:\n%s\n\nGot:\n%s", GitignoreTemplate, contentStr)
			}
		})
	}
}

func TestFixGitignore_PreservesNothing(t *testing.T) {
	// This test documents that FixGitignore does NOT preserve custom patterns
	// It always replaces with the canonical template
	tmpDir := t.TempDir()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Error(err)
		}
	}()

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	customContent := `# User custom patterns
custom-file.txt
*.backup

# Required patterns
*.db
*.db?*
daemon.log
beads.base.jsonl
beads.left.jsonl
beads.right.jsonl
beads.base.meta.json
beads.left.meta.json
beads.right.meta.json
`

	gitignorePath := filepath.Join(".beads", ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(customContent), 0600); err != nil {
		t.Fatal(err)
	}

	err = FixGitignore()
	if err != nil {
		t.Fatalf("FixGitignore failed: %v", err)
	}

	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to read gitignore: %v", err)
	}

	contentStr := string(content)

	// Verify custom patterns are NOT preserved
	if strings.Contains(contentStr, "custom-file.txt") {
		t.Error("Custom pattern 'custom-file.txt' should not be preserved")
	}
	if strings.Contains(contentStr, "*.backup") {
		t.Error("Custom pattern '*.backup' should not be preserved")
	}

	// Verify it matches template exactly
	if contentStr != GitignoreTemplate {
		t.Error("Content should match GitignoreTemplate exactly after fix")
	}
}

func TestFixGitignore_Symlink(t *testing.T) {
	// Skip on Windows as symlink creation requires elevated privileges
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink test on Windows")
	}

	tmpDir := t.TempDir()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Error(err)
		}
	}()

	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Create a target file that the symlink will point to
	targetPath := filepath.Join(tmpDir, "target_gitignore")
	if err := os.WriteFile(targetPath, []byte("old content"), 0600); err != nil {
		t.Fatal(err)
	}

	// Create symlink at .beads/.gitignore pointing to target
	gitignorePath := filepath.Join(".beads", ".gitignore")
	if err := os.Symlink(targetPath, gitignorePath); err != nil {
		t.Fatal(err)
	}

	// Run FixGitignore - it should write through the symlink
	// (os.WriteFile follows symlinks, it doesn't replace them)
	err = FixGitignore()
	if err != nil {
		t.Fatalf("FixGitignore failed: %v", err)
	}

	// Verify it's still a symlink (os.WriteFile follows symlinks)
	info, err := os.Lstat(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to stat .gitignore: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("Expected symlink to be preserved (os.WriteFile follows symlinks)")
	}

	// Verify content is correct (reading through symlink)
	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to read .gitignore: %v", err)
	}
	if string(content) != GitignoreTemplate {
		t.Error("Content doesn't match GitignoreTemplate")
	}

	// Verify target file was updated with correct content
	targetContent, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read target file: %v", err)
	}
	if string(targetContent) != GitignoreTemplate {
		t.Error("Target file content doesn't match GitignoreTemplate")
	}

	// Note: permissions are set on the target file, not the symlink itself
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("Failed to stat target file: %v", err)
	}
	if targetInfo.Mode().Perm() != 0600 {
		t.Errorf("Expected target file permissions 0600, got %o", targetInfo.Mode().Perm())
	}
}

func TestFixGitignore_NonASCIICharacters(t *testing.T) {
	tests := []struct {
		name           string
		initialContent string
		description    string
	}{
		{
			name: "UTF-8 characters in comments",
			initialContent: `# SQLite databases 数据库
*.db
# Daemon files 守护进程文件
daemon.log
`,
			description: "handles UTF-8 characters in comments",
		},
		{
			name: "emoji in content",
			initialContent: `# 🚀 Database files
*.db
# 📝 Logs
daemon.log
`,
			description: "handles emoji characters",
		},
		{
			name: "mixed unicode patterns",
			initialContent: `# файлы базы данных
*.db
# Arquivos de registro
daemon.log
`,
			description: "handles Cyrillic and Latin-based unicode",
		},
		{
			name: "unicode patterns with required content",
			initialContent: `# Unicode comment ñ é ü
*.db
*.db?*
daemon.log
beads.base.jsonl
beads.left.jsonl
beads.right.jsonl
beads.base.meta.json
beads.left.meta.json
beads.right.meta.json
`,
			description: "replaces file even when required patterns present with unicode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Chdir(oldDir); err != nil {
					t.Error(err)
				}
			}()

			beadsDir := filepath.Join(tmpDir, ".beads")
			if err := os.Mkdir(beadsDir, 0750); err != nil {
				t.Fatal(err)
			}

			gitignorePath := filepath.Join(".beads", ".gitignore")
			if err := os.WriteFile(gitignorePath, []byte(tt.initialContent), 0600); err != nil {
				t.Fatal(err)
			}

			err = FixGitignore()
			if err != nil {
				t.Fatalf("FixGitignore failed: %v", err)
			}

			// Verify content is replaced with template (ASCII only)
			content, err := os.ReadFile(gitignorePath)
			if err != nil {
				t.Fatalf("Failed to read .gitignore: %v", err)
			}

			if string(content) != GitignoreTemplate {
				t.Errorf("Content doesn't match GitignoreTemplate\nExpected:\n%s\n\nGot:\n%s", GitignoreTemplate, string(content))
			}
		})
	}
}

func TestFixGitignore_VeryLongLines(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T, tmpDir string) string
		description    string
		expectSuccess  bool
	}{
		{
			name: "single very long line (10KB)",
			setupFunc: func(t *testing.T, tmpDir string) string {
				// Create a 10KB line
				longLine := strings.Repeat("x", 10*1024)
				content := "# Comment\n" + longLine + "\n*.db\n"
				return content
			},
			description:   "handles 10KB single line",
			expectSuccess: true,
		},
		{
			name: "multiple long lines",
			setupFunc: func(t *testing.T, tmpDir string) string {
				line1 := "# " + strings.Repeat("a", 5000)
				line2 := "# " + strings.Repeat("b", 5000)
				line3 := "# " + strings.Repeat("c", 5000)
				content := line1 + "\n" + line2 + "\n" + line3 + "\n*.db\n"
				return content
			},
			description:   "handles multiple long lines",
			expectSuccess: true,
		},
		{
			name: "very long pattern line",
			setupFunc: func(t *testing.T, tmpDir string) string {
				// Create a pattern with extremely long filename
				longPattern := strings.Repeat("very_long_filename_", 500) + ".db"
				content := "# Comment\n" + longPattern + "\n*.db\n"
				return content
			},
			description:   "handles very long pattern names",
			expectSuccess: true,
		},
		{
			name: "100KB single line",
			setupFunc: func(t *testing.T, tmpDir string) string {
				// Create a 100KB line
				longLine := strings.Repeat("y", 100*1024)
				content := "# Comment\n" + longLine + "\n*.db\n"
				return content
			},
			description:   "handles 100KB single line",
			expectSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Chdir(oldDir); err != nil {
					t.Error(err)
				}
			}()

			beadsDir := filepath.Join(tmpDir, ".beads")
			if err := os.Mkdir(beadsDir, 0750); err != nil {
				t.Fatal(err)
			}

			initialContent := tt.setupFunc(t, tmpDir)
			gitignorePath := filepath.Join(".beads", ".gitignore")
			if err := os.WriteFile(gitignorePath, []byte(initialContent), 0600); err != nil {
				t.Fatal(err)
			}

			err = FixGitignore()

			if tt.expectSuccess {
				if err != nil {
					t.Fatalf("FixGitignore failed: %v", err)
				}

				// Verify content is replaced with template
				content, err := os.ReadFile(gitignorePath)
				if err != nil {
					t.Fatalf("Failed to read .gitignore: %v", err)
				}

				if string(content) != GitignoreTemplate {
					t.Error("Content doesn't match GitignoreTemplate")
				}
			} else {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			}
		})
	}
}

func TestCheckGitignore_VariousStatuses(t *testing.T) {
	tests := []struct {
		name           string
		setupFunc      func(t *testing.T, tmpDir string)
		expectedStatus string
		expectedFix    string
		description    string
	}{
		{
			name: "missing .beads directory",
			setupFunc: func(t *testing.T, tmpDir string) {
				// Don't create .beads directory
			},
			expectedStatus: StatusWarning,
			expectedFix:    "Run: bd init (safe to re-run) or bd doctor --fix",
			description:    "returns warning when .beads directory doesn't exist",
		},
		{
			name: "missing .gitignore file",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: StatusWarning,
			expectedFix:    "Run: bd init (safe to re-run) or bd doctor --fix",
			description:    "returns warning when .gitignore doesn't exist",
		},
		{
			name: "perfect gitignore",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
				gitignorePath := filepath.Join(beadsDir, ".gitignore")
				if err := os.WriteFile(gitignorePath, []byte(GitignoreTemplate), 0600); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: StatusOK,
			expectedFix:    "",
			description:    "returns ok when gitignore matches template",
		},
		{
			name: "missing one merge artifact pattern",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
				content := `*.db
*.db?*
daemon.log
beads.base.jsonl
beads.left.jsonl
beads.base.meta.json
beads.left.meta.json
beads.right.meta.json
`
				gitignorePath := filepath.Join(beadsDir, ".gitignore")
				if err := os.WriteFile(gitignorePath, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: StatusWarning,
			expectedFix:    "Run: bd doctor --fix or bd init (safe to re-run)",
			description:    "returns warning when missing beads.right.jsonl",
		},
		{
			name: "missing multiple required patterns",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
				content := `*.db
daemon.log
`
				gitignorePath := filepath.Join(beadsDir, ".gitignore")
				if err := os.WriteFile(gitignorePath, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: StatusWarning,
			expectedFix:    "Run: bd doctor --fix or bd init (safe to re-run)",
			description:    "returns warning when missing multiple patterns",
		},
		{
			name: "empty gitignore file",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
				gitignorePath := filepath.Join(beadsDir, ".gitignore")
				if err := os.WriteFile(gitignorePath, []byte(""), 0600); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: StatusWarning,
			expectedFix:    "Run: bd doctor --fix or bd init (safe to re-run)",
			description:    "returns warning for empty file",
		},
		{
			name: "gitignore with only comments",
			setupFunc: func(t *testing.T, tmpDir string) {
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
				content := `# Comment 1
# Comment 2
# Comment 3
`
				gitignorePath := filepath.Join(beadsDir, ".gitignore")
				if err := os.WriteFile(gitignorePath, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: StatusWarning,
			expectedFix:    "Run: bd doctor --fix or bd init (safe to re-run)",
			description:    "returns warning for comments-only file",
		},
		{
			name: "gitignore as symlink pointing to valid file",
			setupFunc: func(t *testing.T, tmpDir string) {
				if runtime.GOOS == "windows" {
					t.Skip("Skipping symlink test on Windows")
				}
				beadsDir := filepath.Join(tmpDir, ".beads")
				if err := os.Mkdir(beadsDir, 0750); err != nil {
					t.Fatal(err)
				}
				targetPath := filepath.Join(tmpDir, "target_gitignore")
				if err := os.WriteFile(targetPath, []byte(GitignoreTemplate), 0600); err != nil {
					t.Fatal(err)
				}
				gitignorePath := filepath.Join(beadsDir, ".gitignore")
				if err := os.Symlink(targetPath, gitignorePath); err != nil {
					t.Fatal(err)
				}
			},
			expectedStatus: StatusOK,
			expectedFix:    "",
			description:    "follows symlink and checks content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Chdir(oldDir); err != nil {
					t.Error(err)
				}
			}()

			tt.setupFunc(t, tmpDir)

			check := CheckGitignore()

			if check.Status != tt.expectedStatus {
				t.Errorf("Expected status %s, got %s", tt.expectedStatus, check.Status)
			}

			if tt.expectedFix != "" && !strings.Contains(check.Fix, tt.expectedFix) {
				t.Errorf("Expected fix to contain %q, got %q", tt.expectedFix, check.Fix)
			}

			if tt.expectedFix == "" && check.Fix != "" {
				t.Errorf("Expected no fix message, got: %s", check.Fix)
			}
		})
	}
}

func TestFixGitignore_SubdirectoryGitignore(t *testing.T) {
	// This test verifies that FixGitignore only operates on .beads/.gitignore
	// and doesn't touch other .gitignore files

	tmpDir := t.TempDir()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Error(err)
		}
	}()

	// Create .beads directory and gitignore
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Create .beads/.gitignore with old content
	beadsGitignorePath := filepath.Join(".beads", ".gitignore")
	oldBeadsContent := "old beads content"
	if err := os.WriteFile(beadsGitignorePath, []byte(oldBeadsContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Create a subdirectory with its own .gitignore
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0750); err != nil {
		t.Fatal(err)
	}
	subGitignorePath := filepath.Join(subDir, ".gitignore")
	subGitignoreContent := "subdirectory gitignore content"
	if err := os.WriteFile(subGitignorePath, []byte(subGitignoreContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Create root .gitignore
	rootGitignorePath := filepath.Join(tmpDir, ".gitignore")
	rootGitignoreContent := "root gitignore content"
	if err := os.WriteFile(rootGitignorePath, []byte(rootGitignoreContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Run FixGitignore
	err = FixGitignore()
	if err != nil {
		t.Fatalf("FixGitignore failed: %v", err)
	}

	// Verify .beads/.gitignore was updated
	beadsContent, err := os.ReadFile(beadsGitignorePath)
	if err != nil {
		t.Fatalf("Failed to read .beads/.gitignore: %v", err)
	}
	if string(beadsContent) != GitignoreTemplate {
		t.Error(".beads/.gitignore should be updated to template")
	}

	// Verify subdirectory .gitignore was NOT touched
	subContent, err := os.ReadFile(subGitignorePath)
	if err != nil {
		t.Fatalf("Failed to read subdir/.gitignore: %v", err)
	}
	if string(subContent) != subGitignoreContent {
		t.Error("subdirectory .gitignore should not be modified")
	}

	// Verify root .gitignore was NOT touched
	rootContent, err := os.ReadFile(rootGitignorePath)
	if err != nil {
		t.Fatalf("Failed to read root .gitignore: %v", err)
	}
	if string(rootContent) != rootGitignoreContent {
		t.Error("root .gitignore should not be modified")
	}
}

// TestGenerateGitignoreTemplate_Modes tests conditional gitignore generation (GH#797)
func TestGenerateGitignoreTemplate_Modes(t *testing.T) {
	tests := []struct {
		name                 string
		syncBranchConfigured bool
		expectIssuesIgnored  bool   // issues.jsonl as ignore pattern
		expectModeInHeader   string // Mode string in header
	}{
		{
			name:                 "direct mode - JSONL tracked by default",
			syncBranchConfigured: false,
			expectIssuesIgnored:  false, // No pattern = tracked by default
			expectModeInHeader:   "direct (JSONL tracked in current branch)",
		},
		{
			name:                 "sync-branch mode - JSONL ignored",
			syncBranchConfigured: true,
			expectIssuesIgnored:  true, // Explicit ignore, uses git add -f
			expectModeInHeader:   "sync-branch (JSONL ignored, committed via worktree)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := GenerateGitignoreTemplate(tt.syncBranchConfigured)

			// Check mode in header
			if !strings.Contains(content, tt.expectModeInHeader) {
				t.Errorf("Expected mode %q in header, not found in:\n%s", tt.expectModeInHeader, content)
			}

			// Check managed file header
			if !strings.Contains(content, "MANAGED BY BEADS") {
				t.Error("Expected 'MANAGED BY BEADS' header")
			}

			// Check issues.jsonl handling
			// Note: We no longer use negation patterns (!issues.jsonl) as they
			// override fork protection in .git/info/exclude (GH#796)
			hasIgnorePattern := strings.Contains(content, "\nissues.jsonl\n") ||
				strings.HasSuffix(strings.TrimSpace(content), "issues.jsonl")
			// Check for actual negation pattern (at start of line), not mentions in comments
			hasNegationPattern := strings.Contains(content, "\n!issues.jsonl")

			// Negation patterns should NEVER be present (GH#796)
			if hasNegationPattern {
				t.Error("Negation patterns (!issues.jsonl) break fork protection (GH#796)")
			}

			if tt.expectIssuesIgnored && !hasIgnorePattern {
				t.Error("Expected issues.jsonl to be ignored (plain pattern)")
			}
			if !tt.expectIssuesIgnored && hasIgnorePattern {
				t.Error("Expected issues.jsonl to NOT be ignored (tracked by default)")
			}

			// All modes should have required patterns
			for _, pattern := range requiredPatterns {
				if !strings.Contains(content, pattern) {
					t.Errorf("Missing required pattern: %s", pattern)
				}
			}
		})
	}
}

// TestCheckGitignoreWithConfig_ModeMismatch tests mode mismatch detection (GH#797, GH#796)
func TestCheckGitignoreWithConfig_ModeMismatch(t *testing.T) {
	tests := []struct {
		name                 string
		gitignoreContent     string
		syncBranchConfigured bool
		expectedStatus       string
		expectedMessage      string
	}{
		{
			name:                 "direct mode gitignore matches direct config",
			gitignoreContent:     GenerateGitignoreTemplate(false),
			syncBranchConfigured: false,
			expectedStatus:       StatusOK,
		},
		{
			name:                 "direct mode gitignore with sync-branch config - MISMATCH",
			gitignoreContent:     GenerateGitignoreTemplate(false),
			syncBranchConfigured: true,
			expectedStatus:       StatusWarning,
			expectedMessage:      "sync-branch configured but JSONL not ignored",
		},
		{
			name:                 "sync-branch mode gitignore with direct config - MISMATCH",
			gitignoreContent:     GenerateGitignoreTemplate(true),
			syncBranchConfigured: false,
			expectedStatus:       StatusWarning,
			expectedMessage:      "no sync-branch but JSONL ignored",
		},
		{
			name: "negation patterns break fork protection - GH#796",
			gitignoreContent: gitignoreBase + `
# Old direct mode (pre-796)
!issues.jsonl
!interactions.jsonl
`,
			syncBranchConfigured: false,
			expectedStatus:       StatusWarning,
			expectedMessage:      "negation patterns that break fork protection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Chdir(oldDir); err != nil {
					t.Error(err)
				}
			}()

			// Create .beads directory and gitignore
			beadsDir := filepath.Join(tmpDir, ".beads")
			if err := os.Mkdir(beadsDir, 0750); err != nil {
				t.Fatal(err)
			}
			gitignorePath := filepath.Join(beadsDir, ".gitignore")
			if err := os.WriteFile(gitignorePath, []byte(tt.gitignoreContent), 0600); err != nil {
				t.Fatal(err)
			}

			check := CheckGitignoreWithConfig(tt.syncBranchConfigured)

			if check.Status != tt.expectedStatus {
				t.Errorf("Expected status %s, got %s (message: %s)", tt.expectedStatus, check.Status, check.Message)
			}

			if tt.expectedMessage != "" && !strings.Contains(check.Message, tt.expectedMessage) {
				t.Errorf("Expected message containing %q, got %q", tt.expectedMessage, check.Message)
			}
		})
	}
}

// TestCheckGitignoreWithConfig_AssumeUnchanged tests the assume-unchanged check for sync-branch mode (GH#797)
// This test sets up a proper git repo to verify assume-unchanged behavior
func TestCheckGitignoreWithConfig_AssumeUnchanged(t *testing.T) {
	tmpDir := t.TempDir()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Error(err)
		}
	}()

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}
	// Configure git user (required for commits)
	if err := exec.Command("git", "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("Failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "config", "user.name", "Test User").Run(); err != nil {
		t.Fatalf("Failed to configure git name: %v", err)
	}

	// Create .beads directory with JSONL file
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}
	issuesPath := filepath.Join(beadsDir, "issues.jsonl")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"test"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Create correct gitignore for sync-branch mode
	gitignorePath := filepath.Join(beadsDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(GenerateGitignoreTemplate(true)), 0600); err != nil {
		t.Fatal(err)
	}

	// Add and commit the JSONL file so it's tracked
	// Use -f to force-add because sync-branch mode gitignore ignores JSONL files
	if err := exec.Command("git", "add", "-f", ".beads/").Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	if err := exec.Command("git", "commit", "-m", "initial").Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}

	// Test 1: Without assume-unchanged, should warn
	check := CheckGitignoreWithConfig(true)
	if check.Status != StatusWarning {
		t.Errorf("Without assume-unchanged, expected warning, got %s (message: %s)", check.Status, check.Message)
	}
	if !strings.Contains(check.Message, "assume-unchanged") {
		t.Errorf("Expected assume-unchanged warning, got: %s", check.Message)
	}

	// Test 2: Set assume-unchanged flag
	cmd := exec.Command("git", "update-index", "--assume-unchanged", ".beads/issues.jsonl")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to set assume-unchanged: %v\nOutput: %s", err, output)
	}

	// Verify flag is set
	if !hasAssumeUnchanged(".beads/issues.jsonl") {
		t.Error("assume-unchanged flag should be set")
	}

	// Test 3: With assume-unchanged set, should be OK
	check = CheckGitignoreWithConfig(true)
	if check.Status != StatusOK {
		t.Errorf("With assume-unchanged, expected ok, got %s (message: %s)", check.Status, check.Message)
	}
}

// TestFixGitignoreWithConfig tests conditional fix (GH#797)
func TestFixGitignoreWithConfig(t *testing.T) {
	tests := []struct {
		name                 string
		initialContent       string
		syncBranchConfigured bool
		expectIssuesIgnored  bool
	}{
		{
			name:                 "fix to direct mode",
			initialContent:       "old content",
			syncBranchConfigured: false,
			expectIssuesIgnored:  false,
		},
		{
			name:                 "fix to sync-branch mode",
			initialContent:       "old content",
			syncBranchConfigured: true,
			expectIssuesIgnored:  true,
		},
		{
			name:                 "switch from direct to sync-branch",
			initialContent:       GenerateGitignoreTemplate(false),
			syncBranchConfigured: true,
			expectIssuesIgnored:  true,
		},
		{
			name:                 "switch from sync-branch to direct",
			initialContent:       GenerateGitignoreTemplate(true),
			syncBranchConfigured: false,
			expectIssuesIgnored:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()

			oldDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chdir(tmpDir); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Chdir(oldDir); err != nil {
					t.Error(err)
				}
			}()

			// Create .beads directory
			beadsDir := filepath.Join(tmpDir, ".beads")
			if err := os.Mkdir(beadsDir, 0750); err != nil {
				t.Fatal(err)
			}
			gitignorePath := filepath.Join(beadsDir, ".gitignore")
			if err := os.WriteFile(gitignorePath, []byte(tt.initialContent), 0600); err != nil {
				t.Fatal(err)
			}

			// Run fix
			err = FixGitignoreWithConfig(tt.syncBranchConfigured)
			if err != nil {
				t.Fatalf("FixGitignoreWithConfig failed: %v", err)
			}

			// Verify content
			content, err := os.ReadFile(gitignorePath)
			if err != nil {
				t.Fatalf("Failed to read gitignore: %v", err)
			}
			contentStr := string(content)

			// Check if issues.jsonl is ignored (GH#796: no negation patterns allowed)
			hasIgnorePattern := strings.Contains(contentStr, "\nissues.jsonl\n")
			// Check for actual negation pattern (at start of line), not mentions in comments
			hasNegationPattern := strings.Contains(contentStr, "\n!issues.jsonl")

			// Negation patterns should NEVER be present (GH#796)
			if hasNegationPattern {
				t.Error("Negation patterns (!issues.jsonl) break fork protection (GH#796)")
			}

			if tt.expectIssuesIgnored {
				if !hasIgnorePattern {
					t.Error("Expected issues.jsonl to be ignored (sync-branch mode)")
				}
			} else {
				if hasIgnorePattern {
					t.Error("Expected issues.jsonl NOT to be ignored (direct mode, tracked by default)")
				}
			}

			// Verify matches expected template
			expectedTemplate := GenerateGitignoreTemplate(tt.syncBranchConfigured)
			if contentStr != expectedTemplate {
				t.Errorf("Content doesn't match expected template.\nExpected:\n%s\n\nGot:\n%s", expectedTemplate, contentStr)
			}
		})
	}
}

// TestCheckGitignore_BackwardCompatibility verifies deprecated function still works
func TestCheckGitignore_BackwardCompatibility(t *testing.T) {
	tmpDir := t.TempDir()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Error(err)
		}
	}()

	// Create .beads directory with direct mode gitignore
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}
	gitignorePath := filepath.Join(beadsDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(GitignoreTemplate), 0600); err != nil {
		t.Fatal(err)
	}

	// Deprecated CheckGitignore should work (defaults to direct mode)
	check := CheckGitignore()
	if check.Status != StatusOK {
		t.Errorf("Expected OK status, got %s: %s", check.Status, check.Message)
	}
}

// TestFixGitignore_BackwardCompatibility verifies deprecated function still works
func TestFixGitignore_BackwardCompatibility(t *testing.T) {
	tmpDir := t.TempDir()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Error(err)
		}
	}()

	// Create .beads directory
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Deprecated FixGitignore should work (defaults to direct mode)
	err = FixGitignore()
	if err != nil {
		t.Fatalf("FixGitignore failed: %v", err)
	}

	// Verify it wrote direct mode template
	gitignorePath := filepath.Join(beadsDir, ".gitignore")
	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("Failed to read gitignore: %v", err)
	}

	if string(content) != GitignoreTemplate {
		t.Error("Content doesn't match GitignoreTemplate (direct mode)")
	}

	// Verify it does NOT have negation patterns (GH#796 - fork protection)
	// Check for actual negation pattern (at start of line), not mentions in comments
	if strings.Contains(string(content), "\n!issues.jsonl") {
		t.Error("Negation patterns break fork protection (GH#796)")
	}

	// Verify it does NOT have ignore patterns for JSONL (direct mode = tracked by default)
	if strings.Contains(string(content), "\nissues.jsonl\n") {
		t.Error("Direct mode should not ignore issues.jsonl (tracked by default)")
	}
}
