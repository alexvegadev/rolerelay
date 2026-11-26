package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	AppName            = "RoleRelay"
	PortableMarkerFile = "portable.mode"
	DBFilename         = "app.sqlite"
)

type StorageMode string

const (
	StorageModePortable  StorageMode = "portable"
	StorageModeInstalled StorageMode = "installed"
	StorageModeCustom    StorageMode = "custom"
)

type ResolveOptions struct {
	ExecutablePath string
	ForcePortable  bool
	ExplicitDBPath string
	EnvLookup      func(string) (string, bool)
	UserConfigDir  func() (string, error)
}

type StoragePaths struct {
	Mode    StorageMode `json:"mode"`
	DataDir string      `json:"dataDir"`
	DBPath  string      `json:"dbPath"`
	ExeDir  string      `json:"exeDir"`
}

func ResolveStoragePaths(opts ResolveOptions) (StoragePaths, error) {
	lookupEnv := opts.EnvLookup
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	userConfigDir := opts.UserConfigDir
	if userConfigDir == nil {
		userConfigDir = defaultInstalledBaseDir
	}

	exePath := opts.ExecutablePath
	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return StoragePaths{}, fmt.Errorf("resolve executable path: %w", err)
		}
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	exeDir := filepath.Dir(exePath)

	dbOverride := strings.TrimSpace(opts.ExplicitDBPath)
	if dbOverride == "" {
		if v, ok := lookupEnv("ROLERELAY_DB_PATH"); ok {
			dbOverride = strings.TrimSpace(v)
		}
	}
	if dbOverride != "" {
		absDB, err := filepath.Abs(dbOverride)
		if err != nil {
			return StoragePaths{}, fmt.Errorf("resolve custom db path: %w", err)
		}
		return StoragePaths{
			Mode:    StorageModeCustom,
			DataDir: filepath.Dir(absDB),
			DBPath:  absDB,
			ExeDir:  exeDir,
		}, nil
	}

	portable := opts.ForcePortable
	if !portable {
		if v, ok := lookupEnv("ROLERELAY_PORTABLE"); ok {
			v = strings.TrimSpace(strings.ToLower(v))
			if v == "1" || v == "true" || v == "yes" {
				portable = true
			}
		}
	}
	if !portable {
		if fileExists(filepath.Join(exeDir, PortableMarkerFile)) || fileExists(filepath.Join(exeDir, "data", DBFilename)) {
			portable = true
		}
	}

	if portable {
		dataDir := filepath.Join(exeDir, "data")
		return StoragePaths{
			Mode:    StorageModePortable,
			DataDir: dataDir,
			DBPath:  filepath.Join(dataDir, DBFilename),
			ExeDir:  exeDir,
		}, nil
	}

	baseDir, err := userConfigDir()
	if err != nil {
		return StoragePaths{}, fmt.Errorf("resolve user app data directory: %w", err)
	}
	dataDir := filepath.Join(baseDir, AppName)
	return StoragePaths{
		Mode:    StorageModeInstalled,
		DataDir: dataDir,
		DBPath:  filepath.Join(dataDir, DBFilename),
		ExeDir:  exeDir,
	}, nil
}

func EnsureDataDir(paths StoragePaths) error {
	if err := os.MkdirAll(paths.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory %q: %w", paths.DataDir, err)
	}
	return nil
}

func defaultInstalledBaseDir() (string, error) {
	if runtime.GOOS == "windows" {
		if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
			return localAppData, nil
		}
	}
	return os.UserConfigDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
