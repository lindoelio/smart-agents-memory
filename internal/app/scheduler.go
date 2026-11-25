package app

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SchedulerOptions selects one scheduler installation.
type SchedulerOptions struct {
	// Interval is the run interval. Zero means five minutes.
	Interval time.Duration
	// DryRun prints the files without writing.
	DryRun bool
	// Remove removes the scheduler instead of installing it.
	Remove bool
}

// SchedulerResult is the outcome of a scheduler operation.
type SchedulerResult struct {
	// Paths are the files written or removed.
	Paths []string `json:"paths"`
	// Content is the rendered content for a dry run.
	Content string `json:"content,omitempty"`
	// Actions describe what happened.
	Actions []string `json:"actions,omitempty"`
	// DryRun is true when nothing was written.
	DryRun bool `json:"dry_run,omitempty"`
}

// ConfigureScheduler installs or removes the sync scheduler.
func (a *App) ConfigureScheduler(opts SchedulerOptions) (SchedulerResult, error) {
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		exe = "smart-agents-memory"
	}
	if abs, err := filepath.Abs(exe); err == nil {
		exe = abs
	}
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Minute
	}
	if runtime.GOOS == "linux" {
		return configureSystemd(exe, opts)
	}
	return configureLaunchd(exe, opts)
}

func configureLaunchd(exe string, opts SchedulerOptions) (SchedulerResult, error) {
	home := homeDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", "com.smart-agents-memory.sync.plist")
	logDir := filepath.Join(home, "Library", "Logs", "smart-agents-memory")
	logPath := filepath.Join(logDir, "sync.log")
	content := launchdPlist(exe, logPath, opts.Interval)
	result := SchedulerResult{Paths: []string{plist}, Content: content, DryRun: opts.DryRun}
	if opts.Remove {
		for _, p := range result.Paths {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return result, err
			}
		}
		result.Actions = append(result.Actions, "scheduler removed")
		return result, nil
	}
	if opts.DryRun {
		result.Actions = append(result.Actions, "dry run: scheduler not written")
		return result, nil
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return result, err
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return result, err
	}
	if err := os.WriteFile(plist, []byte(content), 0o644); err != nil {
		return result, err
	}
	result.Actions = append(result.Actions, "scheduler installed")
	return result, nil
}

func configureSystemd(exe string, opts SchedulerOptions) (SchedulerResult, error) {
	home := homeDir()
	dir := filepath.Join(home, ".config", "systemd", "user")
	service := filepath.Join(dir, "smart-agents-memory-sync.service")
	timer := filepath.Join(dir, "smart-agents-memory-sync.timer")
	serviceContent, timerContent := systemdUnits(exe, opts.Interval)
	result := SchedulerResult{
		Paths:   []string{service, timer},
		Content: serviceContent + "\n---\n" + timerContent,
		DryRun:  opts.DryRun,
	}
	if opts.Remove {
		for _, p := range result.Paths {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return result, err
			}
		}
		result.Actions = append(result.Actions, "scheduler removed")
		return result, nil
	}
	if opts.DryRun {
		result.Actions = append(result.Actions, "dry run: scheduler not written")
		return result, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return result, err
	}
	if err := os.WriteFile(service, []byte(serviceContent), 0o644); err != nil {
		return result, err
	}
	if err := os.WriteFile(timer, []byte(timerContent), 0o644); err != nil {
		return result, err
	}
	result.Actions = append(result.Actions, "scheduler installed")
	return result, nil
}

func launchdPlist(exe, logPath string, interval time.Duration) string {
	seconds := int(interval.Seconds())
	if seconds <= 0 {
		seconds = 300
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.smart-agents-memory.sync</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>sync</string>
    <string>--quiet</string>
  </array>
  <key>StartInterval</key><integer>%d</integer>
  <key>RunAtLoad</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, html.EscapeString(exe), seconds, html.EscapeString(logPath), html.EscapeString(logPath))
}

func systemdUnits(exe string, interval time.Duration) (string, string) {
	service := fmt.Sprintf(`[Unit]
Description=Smart Agents Memory sync

[Service]
Type=oneshot
ExecStart=%s sync --quiet
`, exe)
	timer := fmt.Sprintf(`[Unit]
Description=Run Smart Agents Memory sync periodically

[Timer]
OnBootSec=1m
OnUnitActiveSec=%s
Persistent=true

[Install]
WantedBy=timers.target
`, interval)
	return service, timer
}
