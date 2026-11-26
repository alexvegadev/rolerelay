package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

var version = "0.1.0"

func main() {
	defaultDir := defaultInstallDir()

	var (
		installDirFlag  = flag.String("install-dir", defaultDir, "Installation directory")
		uninstallFlag   = flag.Bool("uninstall", false, "Uninstall RoleRelay from the installation directory")
		purgeDataFlag   = flag.Bool("purge-data", false, "When uninstalling, also remove user SQLite data directory")
		noLaunchFlag    = flag.Bool("no-launch", false, "Do not launch RoleRelay after installation")
		noShortcutsFlag = flag.Bool("no-shortcuts", false, "Skip creating Start Menu and Desktop shortcuts")
		noRegistryFlag  = flag.Bool("no-registry", false, "Skip writing Windows Add/Remove Programs registry entry")
		silentFlag      = flag.Bool("silent", false, "Run non-interactively")
		versionFlag     = flag.Bool("version", false, "Print installer version")
	)
	flag.Parse()
	_ = silentFlag

	if *versionFlag {
		fmt.Printf("RoleRelay Installer %s\n", version)
		return
	}

	if *uninstallFlag {
		if err := runUninstall(*installDirFlag, *purgeDataFlag, *noShortcutsFlag, *noRegistryFlag); err != nil {
			log.Fatalf("uninstall failed: %v", err)
		}
		fmt.Printf("RoleRelay uninstalled from %s\n", *installDirFlag)
		return
	}

	if len(embeddedAppBinary) == 0 {
		log.Fatalf("installer binary was built without embedded payload; build via scripts/build.ps1")
	}

	if err := runInstall(*installDirFlag, !*noShortcutsFlag, !*noRegistryFlag, !*noLaunchFlag); err != nil {
		log.Fatalf("install failed: %v", err)
	}
	fmt.Printf("RoleRelay %s installed successfully to %s\n", version, *installDirFlag)
}

func defaultInstallDir() string {
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "Programs", "RoleRelay")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "RoleRelay"
	}
	return filepath.Join(home, "RoleRelay")
}

func runInstall(installDir string, makeShortcuts, writeRegistry, launch bool) error {
	absDir, err := filepath.Abs(installDir)
	if err != nil {
		return fmt.Errorf("resolve install dir: %w", err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return fmt.Errorf("create install dir: %w", err)
	}

	appExe := filepath.Join(absDir, "rolerelay.exe")
	if err := writeFileAtomic(appExe, embeddedAppBinary, 0o755); err != nil {
		return fmt.Errorf("write rolerelay.exe: %w", err)
	}

	// Copy the installer executable alongside the app as uninstall.exe.
	uninstallExe := filepath.Join(absDir, "uninstall.exe")
	if selfPath, err := os.Executable(); err == nil {
		if absSelf, err := filepath.Abs(selfPath); err == nil && absSelf != uninstallExe {
			if err := copyFile(absSelf, uninstallExe); err != nil {
				log.Printf("warning: could not copy uninstaller: %v", err)
			}
		}
	}

	if makeShortcuts {
		if sm := startMenuShortcutPath(); sm != "" {
			if err := createShortcut(sm, appExe, absDir, "RoleRelay AI Job Application Assistant"); err != nil {
				log.Printf("warning: Start Menu shortcut failed: %v", err)
			}
		}
		if dk := desktopShortcutPath(); dk != "" {
			if err := createShortcut(dk, appExe, absDir, "RoleRelay AI Job Application Assistant"); err != nil {
				log.Printf("warning: Desktop shortcut failed: %v", err)
			}
		}
	}

	if writeRegistry {
		if err := registerUninstallEntry(absDir, uninstallExe, version); err != nil {
			log.Printf("warning: register uninstall entry failed: %v", err)
		}
	}

	if launch {
		cmd := exec.Command(appExe)
		cmd.Dir = absDir
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("launch installed RoleRelay: %w", err)
		}
	}

	return nil
}

func runUninstall(installDir string, purgeData, skipShortcuts, skipRegistry bool) error {
	absDir, err := filepath.Abs(installDir)
	if err != nil {
		return fmt.Errorf("resolve install dir: %w", err)
	}

	if !skipShortcuts {
		if sm := startMenuShortcutPath(); sm != "" {
			_ = os.Remove(sm)
		}
		if dk := desktopShortcutPath(); dk != "" {
			_ = os.Remove(dk)
		}
	}

	if !skipRegistry {
		if err := removeUninstallEntry(); err != nil {
			log.Printf("warning: remove registry entry: %v", err)
		}
	}

	_ = os.Remove(filepath.Join(absDir, "rolerelay.exe"))
	// Removing uninstall.exe may fail if we are currently running from it on Windows; ignore best-effort.
	_ = os.Remove(filepath.Join(absDir, "uninstall.exe"))
	_ = os.Remove(absDir)

	if purgeData {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			_ = os.RemoveAll(filepath.Join(localAppData, "RoleRelay"))
		}
	}
	return nil
}

func writeFileAtomic(dst string, data []byte, perm os.FileMode) error {
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
