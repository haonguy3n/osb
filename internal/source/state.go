package source

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type State string

const PinTag = "osb/pin"

const (
	StateEmpty State = ""

	StatePin State = "pin"

	StateDev State = "dev"

	StateDevMod State = "dev-mod"

	StateDevDirty State = "dev-dirty"

	StateLocal State = "local"
)

func IsDev(s State) bool {
	return s == StateDev || s == StateDevMod || s == StateDevDirty
}

func DetectState(srcDir string, cached State) (State, error) {
	if _, err := os.Stat(filepath.Join(srcDir, ".git")); err != nil {
		if os.IsNotExist(err) {
			return StateEmpty, nil
		}
		return StateEmpty, err
	}

	dirty, err := stateGit(srcDir, "status", "--porcelain")
	if err != nil {
		return StateDev, err
	}
	if strings.TrimSpace(dirty) != "" {
		return StateDevDirty, nil
	}

	ahead, err := stateGit(srcDir, "rev-list", "--count", PinTag+"..HEAD")
	if err != nil {
		return StateDev, err
	}
	if strings.TrimSpace(ahead) != "0" {
		return StateDevMod, nil
	}

	if cached == StatePin {
		return StatePin, nil
	}
	if cached == StateDev {
		return StateDev, nil
	}
	remote, _ := stateGit(srcDir, "remote", "get-url", "origin")
	if strings.TrimSpace(remote) == "" {
		return StatePin, nil
	}
	return StateDev, nil
}

func stateGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", errors.New(strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}
