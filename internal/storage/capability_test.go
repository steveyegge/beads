package storage_test

import (
	"errors"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
)

func TestCapabilitySet_Has(t *testing.T) {
	s := storage.NewCapabilitySet(storage.CapCRUD, storage.CapSync)
	if !s.Has(storage.CapCRUD) {
		t.Error("expected Has(CapCRUD) = true")
	}
	if !s.Has(storage.CapSync) {
		t.Error("expected Has(CapSync) = true")
	}
	if s.Has(storage.CapPush) {
		t.Error("expected Has(CapPush) = false")
	}
}

func TestCapabilitySet_Require_Present(t *testing.T) {
	s := storage.NewCapabilitySet(storage.CapCRUD)
	if err := s.Require(storage.CapCRUD, "testdriver"); err != nil {
		t.Errorf("Require(present cap) returned unexpected error: %v", err)
	}
}

func TestCapabilitySet_Require_Absent(t *testing.T) {
	s := storage.NewCapabilitySet(storage.CapCRUD)
	err := s.Require(storage.CapVersionControl, "postgres")
	if err == nil {
		t.Fatal("Require(absent cap) expected error, got nil")
	}
	if !errors.Is(err, storage.ErrCapabilityNotSupported) {
		t.Errorf("expected errors.Is(err, ErrCapabilityNotSupported) = true, got %v", err)
	}
}

func TestCapabilitySet_Require_ErrorContainsNames(t *testing.T) {
	s := storage.NewCapabilitySet()
	err := s.Require(storage.CapPush, "mydriver")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !contains(msg, "push") {
		t.Errorf("error message %q does not contain capability name", msg)
	}
	if !contains(msg, "mydriver") {
		t.Errorf("error message %q does not contain driver name", msg)
	}
}

func TestErrCapabilityNotSupported_ErrorsIs(t *testing.T) {
	s := storage.NewCapabilitySet()
	err := s.Require(storage.CapSync, "dolt")
	if !errors.Is(err, storage.ErrCapabilityNotSupported) {
		t.Errorf("errors.Is(err, ErrCapabilityNotSupported) = false for %v", err)
	}
}

func TestDoltCapabilities(t *testing.T) {
	for _, cap := range []storage.Capability{
		storage.CapCRUD,
		storage.CapSchemaInit,
		storage.CapSchemaMigrate,
		storage.CapArchiveJSONL,
		storage.CapRowExport,
		storage.CapRowImport,
		storage.CapVersionControl,
		storage.CapSync,
		storage.CapPush,
		storage.CapPull,
	} {
		if !storage.DoltCapabilities.Has(cap) {
			t.Errorf("DoltCapabilities missing %q", cap)
		}
	}
}

func TestPostgresCapabilities(t *testing.T) {
	present := []storage.Capability{
		storage.CapCRUD,
		storage.CapSchemaInit,
		storage.CapSchemaMigrate,
		storage.CapRowExport,
		storage.CapRowImport,
	}
	absent := []storage.Capability{
		storage.CapVersionControl,
		storage.CapSync,
		storage.CapPush,
		storage.CapPull,
		storage.CapArchiveJSONL,
	}
	for _, cap := range present {
		if !storage.PostgresCapabilities.Has(cap) {
			t.Errorf("PostgresCapabilities missing %q", cap)
		}
	}
	for _, cap := range absent {
		if storage.PostgresCapabilities.Has(cap) {
			t.Errorf("PostgresCapabilities should not have %q", cap)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := range s {
				if i+len(sub) <= len(s) && s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
