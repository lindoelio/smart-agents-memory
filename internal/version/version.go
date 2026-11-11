// Package version holds the single source of truth for the build version.
//
// The release pipeline compares the git tag against this constant and refuses
// to publish when they disagree.
package version

// Version is the current release of Smart Agents Memory.
const Version = "0.1.1"

// Name is the product name, used in help output and user-facing messages.
const Name = "smart-agents-memory"

// UserAgent is sent by outbound HTTP clients (the Notion gateway).
const UserAgent = Name + "/" + Version
