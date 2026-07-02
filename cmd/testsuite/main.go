// Command testsuite runs the suites declared in test-suites.yaml.
// See docs/testing.md for the suite model and requirement probes.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Suite struct {
	Name     string   `yaml:"name"`
	Requires []string `yaml:"requires"`
	Dir      string   `yaml:"dir"`
	Steps    []string `yaml:"steps"`
}

type Config struct {
	Scratch string  `yaml:"scratch"`
	Suites  []Suite `yaml:"suites"`
}

func main() {
	list := flag.Bool("list", false, "list suites and exit")
	file := flag.String("f", "test-suites.yaml", "suites file")
	flag.Parse()

	cfg, err := load(*file)
	if err != nil {
		fatal(err)
	}

	if *list {
		for _, s := range cfg.Suites {
			fmt.Printf("%-18s %d steps  requires: %v\n", s.Name, len(s.Steps), s.Requires)
		}
		return
	}

	selected := cfg.Suites
	if flag.NArg() > 0 {
		selected = nil
		for _, name := range flag.Args() {
			s := find(cfg.Suites, name)
			if s == nil {
				fatal(fmt.Errorf("unknown suite %q (use -list)", name))
			}
			selected = append(selected, *s)
		}
	}

	osbBin, err := buildOsb()
	if err != nil {
		fatal(err)
	}

	type result struct{ name, status string }
	var results []result
	failed := false
	for _, s := range selected {
		if missing := unmet(s.Requires); missing != "" {
			fmt.Printf("\n=== %s: SKIP (missing %s)\n", s.Name, missing)
			results = append(results, result{s.Name, "skip (" + missing + ")"})
			continue
		}
		dir, err := suiteDir(s, cfg.Scratch, osbBin)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("\n=== %s\n", s.Name)
		start := time.Now()
		if err := runSteps(s.Steps, dir, osbBin, cfg.Scratch); err != nil {
			fmt.Printf("=== %s: FAIL (%v)\n", s.Name, err)
			results = append(results, result{s.Name, "FAIL"})
			failed = true
			continue
		}
		results = append(results, result{s.Name, fmt.Sprintf("ok (%s)", time.Since(start).Round(time.Second))})
	}

	fmt.Println("\n--- summary ---")
	for _, r := range results {
		fmt.Printf("%-18s %s\n", r.name, r.status)
	}
	if failed {
		os.Exit(1)
	}
}

func load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func find(suites []Suite, name string) *Suite {
	for i := range suites {
		if suites[i].Name == name {
			return &suites[i]
		}
	}
	return nil
}

func buildOsb() (string, error) {
	bin := filepath.Join(os.TempDir(), "osb-testsuite", "osb")
	if err := os.MkdirAll(filepath.Dir(bin), 0755); err != nil {
		return "", err
	}
	fmt.Println("=== building osb")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/osb")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("building osb: %w", err)
	}
	return bin, nil
}

func unmet(requires []string) string {
	for _, r := range requires {
		ok := false
		switch r {
		case "docker":
			ok = exec.Command("docker", "info").Run() == nil
		case "kvm":
			_, err := os.Stat("/dev/kvm")
			ok = err == nil
		case "binfmt-arm64":
			_, err := os.Stat("/proc/sys/fs/binfmt_misc/qemu-aarch64")
			ok = err == nil
		case "network":
			ok = exec.Command("ping", "-c1", "-W2", "dl-cdn.alpinelinux.org").Run() == nil
		default:
			ok = false
		}
		if !ok {
			return r
		}
	}
	return ""
}

func suiteDir(s Suite, scratch, osbBin string) (string, error) {
	if s.Dir != "scratch" {
		return "", nil
	}
	if _, err := os.Stat(filepath.Join(scratch, "PROJECT.star")); err != nil {
		cmd := exec.Command(osbBin, "init", scratch)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("initializing scratch project: %w", err)
		}
	}
	return scratch, nil
}

func runSteps(steps []string, dir, osbBin, scratch string) error {
	for _, step := range steps {
		fmt.Printf("--- %s\n", step)
		cmd := exec.Command("bash", "-o", "pipefail", "-c", step)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "OSB="+osbBin, "SCRATCH="+scratch)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("step %q: %w", step, err)
		}
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
