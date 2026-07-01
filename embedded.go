// Package embedded holds assets baked into the osb binary at build time.
//
// It lives at the module root because go:embed paths cannot traverse out of
// the embedding file's directory (no ".."), and the canonical asset sources
// (Claude Code skills, the standard-library modules) live at the repository
// root — the same layout a developer edits. Keeping one copy here, rather than
// a mirror under internal/, means the files you edit while working on osb are
// exactly the files the binary ships.
package embedded

import "embed"

// SkillsFS contains the Claude Code skill directories under .claude/skills.
// Read and materialize them via internal/skills; `osb skills install` writes
// them into a project's own .claude/skills directory.
//
//go:embed all:.claude/skills
var SkillsFS embed.FS
