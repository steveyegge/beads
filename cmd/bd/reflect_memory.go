package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/ui"
)

func runMemoryCapture() reflectMemorySummary {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("\nAny lessons learned? [enter to skip]: ")
	lesson, _ := reader.ReadString('\n')
	lesson = strings.TrimSpace(lesson)

	if lesson == "" {
		return reflectMemorySummary{Saved: false}
	}

	fmt.Print("Category? [engineering/debugging/architecture/other]: ")
	category, _ := reader.ReadString('\n')
	category = strings.TrimSpace(category)
	if category == "" {
		category = "other"
	}

	journalPath := getReflectJournalPath()
	if err := appendToJournal(journalPath, lesson, category); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving: %v\n", err)
		return reflectMemorySummary{Saved: false}
	}

	fmt.Printf("%s Saved to %s\n", ui.RenderPassIcon(), journalPath)
	return reflectMemorySummary{Saved: true, Path: journalPath}
}

func getReflectJournalPath() string {
	if configPath := config.GetString("reflect.journal_path"); configPath != "" {
		return os.ExpandEnv(configPath)
	}
	if dbPath != "" {
		beadsDir := filepath.Dir(dbPath)
		return filepath.Join(beadsDir, "reflections.md")
	}
	return filepath.Join(".beads", "reflections.md")
}

func appendToJournal(path, message, category string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	needsHeader := false
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			needsHeader = true
		} else {
			return err
		}
	} else if info.Size() == 0 {
		needsHeader = true
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	if needsHeader {
		if _, err := file.WriteString("# Reflections\n"); err != nil {
			return err
		}
	}

	timestamp := time.Now().Format("2006-01-02 15:04")
	entry := fmt.Sprintf("\n## %s [%s]\n\n%s\n", timestamp, category, message)
	_, err = file.WriteString(entry)
	return err
}
