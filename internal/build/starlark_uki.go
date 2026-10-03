package build

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anhhao17/osb/internal/device"
	osbstar "github.com/anhhao17/osb/internal/starlark"
	"go.starlark.net/starlark"
)

func fnInstallUKI(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		image, kernel, initrd, stub string
		entries                     *starlark.List
		secureboot                  bool
	)
	if err := starlark.UnpackArgs("install_uki", args, kwargs,
		"image", &image, "kernel", &kernel, "initrd", &initrd, "stub?", &stub,
		"entries", &entries, "secureboot?", &secureboot); err != nil {
		return nil, err
	}
	cfg, ok := thread.Local(sandboxKey).(*SandboxConfig)
	if !ok || cfg.DestDir == "" {
		return nil, fmt.Errorf("install_uki() can only be called at build time")
	}
	host := func(p string) (string, error) {
		if p == "" {
			return "", nil
		}
		clean := filepath.Clean(p)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return "", fmt.Errorf("install_uki: %q must be relative to $DESTDIR", p)
		}
		return filepath.Join(cfg.DestDir, clean), nil
	}
	var in device.UKIInputs
	var imgPath string
	var err error
	for _, pair := range []struct {
		dst *string
		src string
	}{{&imgPath, image}, {&in.Kernel, kernel}, {&in.Initrd, initrd}, {&in.Stub, stub}} {
		if *pair.dst, err = host(pair.src); err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(in.Stub); err != nil {
		in.Stub = ""
	}
	boot, err := osbstar.ParseBootEntries(entries)
	if err != nil {
		return nil, err
	}
	keyPEM, certPEM, isTest := device.SecureBootKeyMaterial(cfg.ProjectDir)
	res, err := device.InstallUKIs(imgPath, cfg.Arch, boot, in, keyPEM, certPEM, secureboot)
	if err != nil {
		return nil, err
	}
	w := cfg.Stdout
	if w != nil {
		what := "unsigned"
		if secureboot {
			what = "signed with the project key"
			if isTest {
				what = "signed with the PUBLIC TEST key (run `osb key secure-boot` before shipping)"
			}
		}
		fmt.Fprintf(w, "  UKI %s -> %s\n", what, strings.Join(res.Paths, ", "))
		if res.Verity {
			fmt.Fprintln(w, "  dm-verity root hash folded into the UKI command line")
		}
	}
	return starlark.None, nil
}
