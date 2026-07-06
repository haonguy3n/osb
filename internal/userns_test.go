package internal

import (
	"bytes"
	"strings"
	"testing"
)

func TestUsernsRemediation(t *testing.T) {
	tests := []struct {
		name     string
		apparmor string
		clone    string
		want     string // substring the message must name
	}{
		{
			name:     "apparmor restriction takes precedence",
			apparmor: "1",
			clone:    "0",
			want:     "apparmor_restrict_unprivileged_userns=0",
		},
		{
			name:  "userns_clone gate when apparmor absent",
			clone: "0",
			want:  "unprivileged_userns_clone=1",
		},
		{
			name: "generic kernel guidance when neither knob is set",
			want: "CONFIG_USER_NS=y",
		},
		{
			name:     "no false trigger when apparmor already permits",
			apparmor: "0",
			want:     "CONFIG_USER_NS=y",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := usernsRemediation(tc.apparmor, tc.clone)
			if !strings.Contains(msg, tc.want) {
				t.Errorf("message did not mention %q:\n%s", tc.want, msg)
			}
			// Every variant should reference the observed bwrap symptom.
			if !strings.Contains(msg, "setting up uid map") {
				t.Errorf("message missing the bwrap symptom:\n%s", msg)
			}
		})
	}
}

func TestUsernsWatcher(t *testing.T) {
	tests := []struct {
		name    string
		chunks  []string
		tripped bool
	}{
		{
			name:    "signature in one write",
			chunks:  []string{"bwrap: setting up uid map: Permission denied\n"},
			tripped: true,
		},
		{
			name:    "gid variant",
			chunks:  []string{"bwrap: setting up gid map: Permission denied\n"},
			tripped: true,
		},
		{
			name:    "signature split across writes",
			chunks:  []string{"bwrap: setting up uid ma", "p: Permission denied\n"},
			tripped: true,
		},
		{
			name:    "unrelated stderr never trips",
			chunks:  []string{"configure: error: C compiler cannot create executables\n"},
			tripped: false,
		},
		{
			name:    "unrelated permission-denied never trips",
			chunks:  []string{"bwrap: Can't bind mount /foo: Permission denied\n"},
			tripped: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := &usernsWatcher{w: &buf}
			var written string
			for _, c := range tc.chunks {
				n, err := w.Write([]byte(c))
				if err != nil || n != len(c) {
					t.Fatalf("Write(%q) = %d, %v", c, n, err)
				}
				written += c
			}
			if w.tripped != tc.tripped {
				t.Errorf("tripped = %v, want %v", w.tripped, tc.tripped)
			}
			// The watcher must pass every byte through unchanged.
			if buf.String() != written {
				t.Errorf("pass-through mismatch:\n got %q\nwant %q", buf.String(), written)
			}
		})
	}
}
