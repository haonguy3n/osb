package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

func RunInit(projectDir string, machine string) error {
	if _, err := os.Stat(filepath.Join(projectDir, "PROJECT.star")); err == nil {
		return fmt.Errorf("project already exists at %s (PROJECT.star found)", projectDir)
	}

	// The loader's phases: machines, units, then images — image definitions
	// go in images/ so their closures resolve against every module's units.
	dirs := []string{"machines", "units", "images", "classes"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(projectDir, dir), 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	name := filepath.Base(projectDir)
	defaultMachine := machine
	if defaultMachine == "" {
		defaultMachine = "qemu-x86_64"
	}

	projectContent := fmt.Sprintf(`project(
    name = %q,
    version = "0.1.0",
    # What a bare `+"`osb build` / `osb run`"+` targets. defaults.distro is the
    # fallback for any image that doesn't set its own distro (cascade:
    # image.distro -> local.default_distro_override -> defaults.distro).
    defaults = defaults(
        machine = %q,
        image = "base-image",
        distro = "alpine",
    ),

    # osb bundles its standard library (core recipes, machines, images, and
    # the alpine/debian/ubuntu feeds) into the binary, so no modules or pins
    # are needed here. Optional fields for growing the project:
    #
    #   modules = [...]         external module repos; evaluated after the
    #                           bundled stdlib, so same-named units shadow it
    #                           (or just drop .star files under this project's
    #                           units/, machines/, images/, classes/).
    #
    #   prefer_modules = {...}  per-distro pins overriding name resolution,
    #                           e.g. {"alpine": {"xz": "alpine.main"}}. The
    #                           stdlib distro modules already ship the pins
    #                           their feeds require; an entry here overrides
    #                           per unit, and pinning to "" restores default
    #                           module-priority resolution.
)
`, name, defaultMachine)

	if err := os.WriteFile(filepath.Join(projectDir, "PROJECT.star"), []byte(projectContent), 0644); err != nil {
		return fmt.Errorf("writing PROJECT.star: %w", err)
	}

	// Create .gitignore covering everything osb generates in a project tree:
	// build output, the module/source cache, the local apk repository, and the
	// per-developer local.star overrides. .claude/skills is intentionally not
	// ignored — those are project skills meant to be committed — but Claude
	// Code's per-user settings.local.json is.
	gitignore := `# Build output, caches, and the local apk repository
/build
/cache
/repo

# Per-developer settings written by osb (machine, image, parallel builds)
local.star

# Claude Code per-user local settings (skills under .claude/ are committed)
.claude/settings.local.json
`
	if err := os.WriteFile(filepath.Join(projectDir, ".gitignore"), []byte(gitignore), 0644); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}

	fmt.Printf("Created Osb project at %s\n", projectDir)

	return nil
}
