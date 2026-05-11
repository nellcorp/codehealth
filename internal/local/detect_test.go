package local

import (
	"errors"
	"testing"
)

func TestDetectStrictMissing(t *testing.T) {
	// Use a binary name guaranteed not to exist on PATH.
	b, err := DetectStrict("codehealth-no-such-cs-binary-xyzzy")
	if !errors.Is(err, ErrCSNotFound) {
		t.Fatalf("want ErrCSNotFound, got %v", err)
	}
	if b != nil {
		t.Fatalf("want nil backend, got %T", b)
	}
}

func TestDetectStrictPresent(t *testing.T) {
	// `go` is reliably on PATH in this repo's CI and dev env; treat as a
	// stand-in for any resolvable binary.
	b, err := DetectStrict("go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b == nil || b.Name() != "cs" {
		t.Fatalf("want cs backend, got %v", b)
	}
}

func TestDetectFallback(t *testing.T) {
	// Detect must keep its silent-fallback contract for callers that opt
	// in to graceful degradation.
	prev := WarnFallbackTo
	defer func() { WarnFallbackTo = prev }()
	WarnFallbackTo = func(string) {}
	// Force fallback by pointing at a non-existent binary; the package
	// uses sync.Once for the warning so we just check the backend.
	b := Detect("codehealth-no-such-cs-binary-xyzzy")
	if b == nil || b.Name() != "go-fallback" {
		t.Fatalf("want go-fallback backend, got %v", b)
	}
}
