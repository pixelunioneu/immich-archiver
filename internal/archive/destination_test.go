package archive

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDestinationNewFile(t *testing.T) {
	dir := t.TempDir()
	name, exists, err := ResolveDestination(dir, "IMG_0001.jpg", "asset-1")
	if err != nil {
		t.Fatalf("ResolveDestination: %v", err)
	}
	if exists {
		t.Fatal("expected new file to not already exist")
	}
	if name != "IMG_0001.jpg" {
		t.Fatalf("got %q, want IMG_0001.jpg", name)
	}
}

func TestResolveDestinationAlreadyDownloaded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "IMG_0001.jpg"), "data")
	writeFile(t, filepath.Join(dir, "IMG_0001.jpg.json"), `{"id":"asset-1"}`)

	name, exists, err := ResolveDestination(dir, "IMG_0001.jpg", "asset-1")
	if err != nil {
		t.Fatalf("ResolveDestination: %v", err)
	}
	if !exists {
		t.Fatal("expected asset to be detected as already downloaded")
	}
	if name != "IMG_0001.jpg" {
		t.Fatalf("got %q, want IMG_0001.jpg", name)
	}
}

func TestResolveDestinationCollisionDifferentAsset(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "IMG_0001.jpg"), "data")
	writeFile(t, filepath.Join(dir, "IMG_0001.jpg.json"), `{"id":"asset-OTHER"}`)

	name, exists, err := ResolveDestination(dir, "IMG_0001.jpg", "asset-1")
	if err != nil {
		t.Fatalf("ResolveDestination: %v", err)
	}
	if exists {
		t.Fatal("expected collision with a different asset to not be treated as already-downloaded")
	}
	if name != "IMG_0001_1.jpg" {
		t.Fatalf("got %q, want IMG_0001_1.jpg", name)
	}
}

func TestResolveDestinationMultipleCollisions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "IMG_0001.jpg"), "data")
	writeFile(t, filepath.Join(dir, "IMG_0001.jpg.json"), `{"id":"asset-OTHER-1"}`)
	writeFile(t, filepath.Join(dir, "IMG_0001_1.jpg"), "data")
	writeFile(t, filepath.Join(dir, "IMG_0001_1.jpg.json"), `{"id":"asset-OTHER-2"}`)

	name, exists, err := ResolveDestination(dir, "IMG_0001.jpg", "asset-1")
	if err != nil {
		t.Fatalf("ResolveDestination: %v", err)
	}
	if exists {
		t.Fatal("expected no match")
	}
	if name != "IMG_0001_2.jpg" {
		t.Fatalf("got %q, want IMG_0001_2.jpg", name)
	}
}

func TestResolveDestinationMissingSidecarTreatedAsCollision(t *testing.T) {
	dir := t.TempDir()
	// File exists but no sidecar at all (e.g. foreign file dropped in the folder).
	writeFile(t, filepath.Join(dir, "IMG_0001.jpg"), "data")

	name, exists, err := ResolveDestination(dir, "IMG_0001.jpg", "asset-1")
	if err != nil {
		t.Fatalf("ResolveDestination: %v", err)
	}
	if exists {
		t.Fatal("expected no match since sidecar is missing")
	}
	if name != "IMG_0001_1.jpg" {
		t.Fatalf("got %q, want IMG_0001_1.jpg", name)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
