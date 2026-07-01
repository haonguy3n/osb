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

// StdlibFS contains the bundled standard-library modules under stdlib/ —
// module-core, module-bsp, and the alpine/debian/ubuntu feed declarations.
// osb materializes these to a per-user cache directory (see internal/stdlib)
// and injects them as implicit lowest-priority modules, so a fresh project
// builds with no external module repositories.
//
// Feed index data (APKINDEX, Packages) is deliberately excluded from stdlib/
// and fetched on demand via `osb update-feeds`; embedding a snapshot would go
// stale and 404 the moment upstream rotated a package.
//
//go:embed all:stdlib
var StdlibFS embed.FS
