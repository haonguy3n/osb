package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/anhhao17/osb/internal/build"
)

func cmdLog(args []string) {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	edit := fs.Bool("e", false, "open log in editor")
	fs.Parse(args)

	dir := projectDir()
	unitName := fs.Arg(0)
	var logPath string

	if unitName != "" {
		buildDir, derr := unitBuildDirForCWD(dir, unitName)
		if derr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", derr)
			os.Exit(1)
		}
		logPath = filepath.Join(buildDir, "build.log")
	} else {
		logPath = findLatestBuildLog(dir)
	}

	if logPath == "" {
		fmt.Fprintln(os.Stderr, "No build logs found")
		os.Exit(1)
	}

	if *edit {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		cmd := exec.Command(editor, logPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	os.Stdout.Write(data)
}

// findLatestBuildLog returns the newest build.log under build/<arch>/, or "" if
// none exist. Used by `osb log` with no unit argument.
func findLatestBuildLog(projectDir string) string {
	archDir := filepath.Join(projectDir, "build", build.Arch())
	entries, err := os.ReadDir(archDir)
	if err != nil {
		return ""
	}

	type logEntry struct {
		path    string
		modTime int64
	}
	var logs []logEntry

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(archDir, e.Name(), "build.log")
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		logs = append(logs, logEntry{p, info.ModTime().UnixNano()})
	}

	if len(logs) == 0 {
		return ""
	}

	sort.Slice(logs, func(i, j int) bool {
		return logs[i].modTime > logs[j].modTime
	})
	return logs[0].path
}
