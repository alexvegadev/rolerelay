//go:build !windows

package main

func registerUninstallEntry(_, _, _ string) error { return nil }
func removeUninstallEntry() error                 { return nil }
func createShortcut(_, _, _, _ string) error      { return nil }
func startMenuShortcutPath() string               { return "" }
func desktopShortcutPath() string                 { return "" }
