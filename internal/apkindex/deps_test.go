package apkindex

import (
	"testing"
)

func TestParseDep_Forms(t *testing.T) {
	cases := []struct {
		in       string
		kind     DepKind
		name     string
		op       Op
		version  string
		conflict bool
	}{
		{"musl", DepKindName, "musl", OpNone, "", false},
		{"musl>=1.2", DepKindName, "musl", OpGe, "1.2", false},
		{"musl<2", DepKindName, "musl", OpLt, "2", false},
		{"musl<=1.2", DepKindName, "musl", OpLe, "1.2", false},
		{"musl>1", DepKindName, "musl", OpGt, "1", false},
		{"musl=1.2.3-r0", DepKindName, "musl", OpEq, "1.2.3-r0", false},
		{"musl~1.2", DepKindName, "musl", OpTilde, "1.2", false},
		{"so:libcrypto.so.3", DepKindSo, "so:libcrypto.so.3", OpNone, "", false},
		{"so:libcrypto.so.3=3.5.4-r0", DepKindSo, "so:libcrypto.so.3", OpEq, "3.5.4-r0", false},
		{"cmd:gpg", DepKindCmd, "cmd:gpg", OpNone, "", false},
		{"cmd:gpg=2.0", DepKindCmd, "cmd:gpg", OpEq, "2.0", false},
		{"pc:libfoo", DepKindPc, "pc:libfoo", OpNone, "", false},
		{"/etc/passwd", DepKindPath, "/etc/passwd", OpNone, "", false},
		{"!busybox", DepKindName, "busybox", OpNone, "", true},
		{"!so:libold.so.1", DepKindSo, "so:libold.so.1", OpNone, "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			d, err := ParseDep(c.in)
			if err != nil {
				t.Fatalf("ParseDep(%q): %v", c.in, err)
			}
			if d.Kind != c.kind {
				t.Errorf("Kind: got %d, want %d", d.Kind, c.kind)
			}
			if d.Name != c.name {
				t.Errorf("Name: got %q, want %q", d.Name, c.name)
			}
			if d.Op != c.op {
				t.Errorf("Op: got %d, want %d", d.Op, c.op)
			}
			if d.Version != c.version {
				t.Errorf("Version: got %q, want %q", d.Version, c.version)
			}
			if d.Conflict != c.conflict {
				t.Errorf("Conflict: got %v, want %v", d.Conflict, c.conflict)
			}
			if d.Raw != c.in {
				t.Errorf("Raw: got %q, want %q", d.Raw, c.in)
			}
		})
	}
}

func TestParseDep_Errors(t *testing.T) {
	cases := []string{"", "!", "<1.0"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if _, err := ParseDep(c); err == nil {
				t.Errorf("ParseDep(%q): want error", c)
			}
		})
	}
}
