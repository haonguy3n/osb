package repo

import (
	"path/filepath"
	"testing"

	osbstar "github.com/anhhao17/osb/internal/starlark"
)

func TestRepoDir_IncludesProjectName(t *testing.T) {
	proj := &osbstar.Project{Name: "my-product"}
	got := RepoDir(proj, "/home/user/project")
	want := filepath.Join("/home/user/project", "repo", "my-product")
	if got != want {
		t.Errorf("RepoDir = %q, want %q", got, want)
	}
}

func TestRepoDir_NilProject(t *testing.T) {
	got := RepoDir(nil, "/home/user/project")
	want := filepath.Join("/home/user/project", "repo")
	if got != want {
		t.Errorf("RepoDir = %q, want %q", got, want)
	}
}
