package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveStoragePaths(t *testing.T) {
	t.Run("installed default when no portable marker", func(t *testing.T) {
		tmp := t.TempDir()
		exePath := filepath.Join(tmp, "bin", "rolerelay.exe")
		appData := filepath.Join(tmp, "LocalAppData")

		paths, err := ResolveStoragePaths(ResolveOptions{
			ExecutablePath: exePath,
			EnvLookup:      func(string) (string, bool) { return "", false },
			UserConfigDir:  func() (string, error) { return appData, nil },
		})
		if err != nil {
			t.Fatalf("ResolveStoragePaths returned error: %v", err)
		}
		if paths.Mode != StorageModeInstalled {
			t.Fatalf("expected installed mode, got %q", paths.Mode)
		}
		wantDB := filepath.Join(appData, AppName, DBFilename)
		if paths.DBPath != wantDB {
			t.Fatalf("expected db path %q, got %q", wantDB, paths.DBPath)
		}
	})

	t.Run("portable mode detected via portable.mode marker", func(t *testing.T) {
		tmp := t.TempDir()
		exeDir := filepath.Join(tmp, "portable-app")
		if err := os.MkdirAll(exeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(exeDir, PortableMarkerFile), []byte("portable\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		paths, err := ResolveStoragePaths(ResolveOptions{
			ExecutablePath: filepath.Join(exeDir, "rolerelay.exe"),
			EnvLookup:      func(string) (string, bool) { return "", false },
			UserConfigDir:  func() (string, error) { return filepath.Join(tmp, "LocalAppData"), nil },
		})
		if err != nil {
			t.Fatalf("ResolveStoragePaths returned error: %v", err)
		}
		if paths.Mode != StorageModePortable {
			t.Fatalf("expected portable mode, got %q", paths.Mode)
		}
		wantDB := filepath.Join(exeDir, "data", DBFilename)
		if paths.DBPath != wantDB {
			t.Fatalf("expected db path %q, got %q", wantDB, paths.DBPath)
		}
	})

	t.Run("portable mode detected via existing data/app.sqlite", func(t *testing.T) {
		tmp := t.TempDir()
		exeDir := filepath.Join(tmp, "portable-existing")
		dataDir := filepath.Join(exeDir, "data")
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, DBFilename), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}

		paths, err := ResolveStoragePaths(ResolveOptions{
			ExecutablePath: filepath.Join(exeDir, "rolerelay.exe"),
			EnvLookup:      func(string) (string, bool) { return "", false },
			UserConfigDir:  func() (string, error) { return filepath.Join(tmp, "LocalAppData"), nil },
		})
		if err != nil {
			t.Fatalf("ResolveStoragePaths returned error: %v", err)
		}
		if paths.Mode != StorageModePortable {
			t.Fatalf("expected portable mode, got %q", paths.Mode)
		}
	})

	t.Run("explicit db path overrides mode", func(t *testing.T) {
		tmp := t.TempDir()
		customDB := filepath.Join(tmp, "custom", "my.sqlite")
		paths, err := ResolveStoragePaths(ResolveOptions{
			ExecutablePath: filepath.Join(tmp, "rolerelay.exe"),
			ExplicitDBPath: customDB,
			EnvLookup:      func(string) (string, bool) { return "", false },
		})
		if err != nil {
			t.Fatalf("ResolveStoragePaths returned error: %v", err)
		}
		if paths.Mode != StorageModeCustom {
			t.Fatalf("expected custom mode, got %q", paths.Mode)
		}
		if paths.DBPath != customDB {
			t.Fatalf("expected %q, got %q", customDB, paths.DBPath)
		}
	})
}
