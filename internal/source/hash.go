package source

import (
	"crypto/sha256"
	"fmt"
)

func SrcHashInputs(srcDir string, state State) string {
	if !IsDev(state) {
		return ""
	}
	head, err := stateGit(srcDir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	headSha := trim(head)
	if state == StateDevDirty {
		diff, err := stateGit(srcDir, "diff", "HEAD")
		if err != nil {
			return "head:" + headSha
		}
		porcelain, _ := stateGit(srcDir, "status", "--porcelain")
		sum := sha256.Sum256([]byte(diff + "\x00" + porcelain))
		return fmt.Sprintf("head:%s:dirty:%x", headSha, sum[:8])
	}
	return "head:" + headSha
}

func SrcDescribe(srcDir string) string {
	out, err := stateGit(srcDir, "describe", "--dirty", "--always", "--tags")
	if err != nil {
		return ""
	}
	return trim(out)
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
