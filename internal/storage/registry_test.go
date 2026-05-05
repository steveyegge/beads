package storage

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// resetRegistry clears the registry between subtests. Only callable from the
// storage package's tests; not exported.
func resetRegistry() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registryEntries = map[BackendType]Factory{}
}

func TestRegisterAndOpen(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	var captured ConnectionConfig
	RegisterDriver("test-backend", func(_ context.Context, cfg ConnectionConfig) (Storage, error) {
		captured = cfg
		return nil, nil
	})

	in := ConnectionConfig{BeadsDir: "/tmp/x", Database: "beads", ReadOnly: true, DSN: "ignored"}
	if _, err := Open(context.Background(), "test-backend", in); err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if !reflect.DeepEqual(captured, in) {
		t.Fatalf("factory received %+v, want %+v", captured, in)
	}
}

func TestOpenUnknownBackend(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	RegisterDriver("alpha", func(_ context.Context, _ ConnectionConfig) (Storage, error) {
		return nil, nil
	})
	RegisterDriver("zulu", func(_ context.Context, _ ConnectionConfig) (Storage, error) {
		return nil, nil
	})

	_, err := Open(context.Background(), "nope", ConnectionConfig{})
	if err == nil {
		t.Fatal("Open with unregistered backend returned nil error")
	}
	var unk *ErrUnknownBackend
	if !errors.As(err, &unk) {
		t.Fatalf("expected *ErrUnknownBackend, got %T: %v", err, err)
	}
	if unk.Name != "nope" {
		t.Errorf("unk.Name = %q, want %q", unk.Name, "nope")
	}
	want := []BackendType{"alpha", "zulu"}
	if !reflect.DeepEqual(unk.Available, want) {
		t.Errorf("unk.Available = %v, want sorted %v", unk.Available, want)
	}
}

func TestRegisteredBackendsSorted(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	for _, n := range []BackendType{"zulu", "alpha", "mike"} {
		RegisterDriver(n, func(_ context.Context, _ ConnectionConfig) (Storage, error) {
			return nil, nil
		})
	}

	got := RegisteredBackends()
	want := []BackendType{"alpha", "mike", "zulu"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RegisteredBackends() = %v, want %v", got, want)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	RegisterDriver("dup", func(_ context.Context, _ ConnectionConfig) (Storage, error) {
		return nil, nil
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate RegisterDriver, got none")
		}
	}()
	RegisterDriver("dup", func(_ context.Context, _ ConnectionConfig) (Storage, error) {
		return nil, nil
	})
}

func TestRegisterEmptyNamePanics(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty name, got none")
		}
	}()
	RegisterDriver("", func(_ context.Context, _ ConnectionConfig) (Storage, error) {
		return nil, nil
	})
}

func TestRegisterNilFactoryPanics(t *testing.T) {
	resetRegistry()
	t.Cleanup(resetRegistry)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil factory, got none")
		}
	}()
	RegisterDriver("nilfact", nil)
}
