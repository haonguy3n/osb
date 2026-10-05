package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func BuildTemplateContext(u *osbstar.Unit, arch, machine, console, project, projectVersion, baseDistro, baseVersion string) map[string]any {
	m := map[string]any{
		"name":            u.Name,
		"version":         u.Version,
		"release":         int64(u.Release),
		"arch":            arch,
		"machine":         machine,
		"console":         console,
		"project":         project,
		"project_version": projectVersion,
		"base_distro":     baseDistro,
		"base_version":    baseVersion,
	}
	for k, v := range u.Extra {
		m[k] = v
	}
	return m
}

func doInstallStep(u *osbstar.Unit, step *osbstar.InstallStep, data map[string]any, env map[string]string) error {
	srcPath, err := resolveTemplatePath(u, step)
	if err != nil {
		return fmt.Errorf("install %s: %w", step.Src, err)
	}
	destPath := expandEnv(step.Dest, env)

	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("install %s: reading %s: %w", step.Src, srcPath, err)
	}

	var out []byte
	switch step.Kind {
	case "file":
		out = raw
	case "template":
		tmpl, err := template.New(filepath.Base(srcPath)).
			Option("missingkey=error").
			Parse(string(raw))
		if err != nil {
			return fmt.Errorf("install_template %s: parsing: %w", srcPath, err)
		}
		var buf strings.Builder
		if err := tmpl.Execute(&buf, data); err != nil {
			return fmt.Errorf("install_template %s: rendering: %w", srcPath, err)
		}
		out = []byte(buf.String())
	default:
		return fmt.Errorf("install %s: unknown kind %q", step.Src, step.Kind)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("install %s: creating dest dir: %w", step.Src, err)
	}
	if err := os.WriteFile(destPath, out, os.FileMode(step.Mode)); err != nil {
		return fmt.Errorf("install %s: writing %s: %w", step.Src, destPath, err)
	}
	return nil
}

func installStepLabel(s *osbstar.InstallStep) string {
	fn := "install_file"
	if s.Kind == "template" {
		fn = "install_template"
	}
	return fmt.Sprintf("%s: %s -> %s", fn, s.Src, s.Dest)
}

func resolveTemplatePath(u *osbstar.Unit, step *osbstar.InstallStep) (string, error) {
	baseDir := step.BaseDir
	if baseDir == "" {
		baseDir = filepath.Join(u.DefinedIn, u.Name)
	}
	resolved := filepath.Join(baseDir, step.Src)
	rel, err := filepath.Rel(baseDir, resolved)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", step.Src, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes install base directory", step.Src)
	}
	return resolved, nil
}

func expandEnv(s string, env map[string]string) string {
	return os.Expand(s, func(key string) string {
		return env[key]
	})
}
