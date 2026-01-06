package beads

import (
	"testing"
)

func TestDetectFormat_TOON(t *testing.T) {
	data := []byte("issues[\n  {id: \"bd-1\", title: \"Test\"}\n]")
	format, err := DetectFormat(data)
	if err != nil {
		t.Fatalf("DetectFormat failed: %v", err)
	}
	if format != FormatTOON {
		t.Errorf("expected FormatTOON, got %v", format)
	}
}

func TestDetectFormat_JSONL_Object(t *testing.T) {
	data := []byte("{\"id\": \"bd-1\", \"title\": \"Test\"}")
	format, err := DetectFormat(data)
	if err != nil {
		t.Fatalf("DetectFormat failed: %v", err)
	}
	if format != FormatJSONL {
		t.Errorf("expected FormatJSONL, got %v", format)
	}
}

func TestDetectFormat_JSONL_Array(t *testing.T) {
	data := []byte("[{\"id\": \"bd-1\", \"title\": \"Test\"}]")
	format, err := DetectFormat(data)
	if err != nil {
		t.Fatalf("DetectFormat failed: %v", err)
	}
	if format != FormatJSONL {
		t.Errorf("expected FormatJSONL, got %v", format)
	}
}

func TestDetectFormat_Empty(t *testing.T) {
	data := []byte("")
	_, err := DetectFormat(data)
	if err == nil {
		t.Errorf("expected error for empty data, got nil")
	}
}

func TestDetectFormat_OnlyWhitespace(t *testing.T) {
	data := []byte("   \n\t  ")
	_, err := DetectFormat(data)
	if err == nil {
		t.Errorf("expected error for whitespace-only data, got nil")
	}
}
