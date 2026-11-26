//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const uninstallRegPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\RoleRelay`

func registerUninstallEntry(installDir, uninstallExe, appVersion string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallRegPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create uninstall registry key: %w", err)
	}
	defer key.Close()

	uninstallCmd := fmt.Sprintf(`"%s" --uninstall --install-dir "%s"`, uninstallExe, installDir)
	values := map[string]string{
		"DisplayName":     "RoleRelay",
		"DisplayVersion":  appVersion,
		"Publisher":       "RoleRelay",
		"InstallLocation": installDir,
		"UninstallString": uninstallCmd,
		"DisplayIcon":     filepath.Join(installDir, "rolerelay.exe"),
	}
	for k, v := range values {
		if err := key.SetStringValue(k, v); err != nil {
			return fmt.Errorf("set registry value %s: %w", k, err)
		}
	}
	_ = key.SetDWordValue("NoModify", 1)
	_ = key.SetDWordValue("NoRepair", 1)
	return nil
}

func removeUninstallEntry() error {
	err := registry.DeleteKey(registry.CURRENT_USER, uninstallRegPath)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}

func createShortcut(shortcutPath, targetPath, workingDir, description string) error {
	if err := os.MkdirAll(filepath.Dir(shortcutPath), 0o755); err != nil {
		return fmt.Errorf("create shortcut directory: %w", err)
	}

	escapePS := func(s string) string {
		return strings.ReplaceAll(s, "'", "''")
	}
	script := fmt.Sprintf(
		"$ws = New-Object -ComObject WScript.Shell; $s = $ws.CreateShortcut('%s'); $s.TargetPath = '%s'; $s.WorkingDirectory = '%s'; $s.Description = '%s'; $s.Save()",
		escapePS(shortcutPath),
		escapePS(targetPath),
		escapePS(workingDir),
		escapePS(description),
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create shortcut %q: %w (%s)", shortcutPath, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func startMenuShortcutPath() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "RoleRelay.lnk")
}

func desktopShortcutPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Desktop", "RoleRelay.lnk")
}
