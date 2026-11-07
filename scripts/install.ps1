# Installs Smart Agents Memory from a clone of the repository, on Windows.
#
# The release channel is Homebrew and the GitHub releases. This script is
# for the machine that wants to build from source.
#
# Usage:
#   scripts\install.ps1 [-MemoryHome <path>]
param(
    [string]$MemoryHome = ""
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$binDir = Join-Path $env:USERPROFILE ".local\bin"

Write-Host "==> Building the binary"
Push-Location $root
try {
    go build -trimpath -ldflags "-s -w" -o (Join-Path $binDir "smart-agents-memory.exe") ./cmd/smart-agents-memory
} finally {
    Pop-Location
}

$binary = Join-Path $binDir "smart-agents-memory.exe"
Write-Host "==> Installed $((& $binary version).Trim()) in $binDir"

if ($MemoryHome -ne "") {
    $env:SMART_AGENTS_MEMORY_HOME = $MemoryHome
}

Write-Host "==> Linking the tools to the store"
& $binary install

Write-Host ""
Write-Host "==> Done. Check the installation with:"
Write-Host "    smart-agents-memory doctor"
