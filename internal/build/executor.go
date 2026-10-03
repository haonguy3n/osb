package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	osb "github.com/anhhao17/osb/internal"
	"github.com/anhhao17/osb/internal/artifact"
	"github.com/anhhao17/osb/internal/deb"
	"github.com/anhhao17/osb/internal/device"
	"github.com/anhhao17/osb/internal/repo"
	"github.com/anhhao17/osb/internal/resolve"
	"github.com/anhhao17/osb/internal/sbom"
	"github.com/anhhao17/osb/internal/source"
	osbstar "github.com/anhhao17/osb/internal/starlark"
	"go.starlark.net/starlark"
)

const DefaultParallel = osbstar.DefaultParallelBuilds

type BuildEvent struct {
	Unit   string
	Status string
}

type Options struct {
	Ctx             context.Context
	Force           bool
	Clean           bool
	NoCache         bool
	DryRun          bool
	Verbose         bool
	ProjectDir      string
	Arch            string
	Machine         string
	ProjectCommit   string
	Signer          *artifact.Signer
	OnEvent         func(BuildEvent)
	Parallel        int
	EffectiveDistro string
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

func ScopeDir(unit *osbstar.Unit, arch, machine string) string {
	switch unit.Scope {
	case "machine":
		return machine
	case "noarch":
		return "noarch"
	default:
		return arch
	}
}

func RepoArchDir(unit *osbstar.Unit, arch string) string {
	if unit.Scope == "noarch" {
		return "noarch"
	}
	return ApkArch(arch)
}

func ApkArch(arch string) string {
	if arch == "arm64" {
		return "aarch64"
	}
	return arch
}

func BuildUnits(proj *osbstar.Project, names []string, opts Options, w io.Writer) error {
	if opts.ProjectCommit == "" {
		opts.ProjectCommit = readProjectCommit(opts.ProjectDir)
	}

	if opts.Signer == nil {
		signer, err := artifact.LoadOrGenerateSigner(proj.Name, proj.SigningKey)
		if err != nil {
			return fmt.Errorf("loading signing key: %w", err)
		}
		opts.Signer = signer
	}

	effectiveDistro := opts.EffectiveDistro
	if effectiveDistro == "" {
		var derr error
		effectiveDistro, derr = proj.EffectiveDistro()
		if derr != nil {
			return fmt.Errorf("resolving effective distro for build: %w", derr)
		}
		opts.EffectiveDistro = effectiveDistro
	}
	repoDir := repo.RepoDistroDir(proj, opts.ProjectDir, effectiveDistro)
	if err := repo.WritePublicKey(repoDir, opts.Signer); err != nil {
		return fmt.Errorf("publishing project public key: %w", err)
	}

	dag, err := resolve.BuildDAG(proj, effectiveDistro)
	if err != nil {
		return err
	}

	order, err := dag.TopologicalSort()
	if err != nil {
		return err
	}

	srcInputs := SrcInputsFn(opts.ProjectDir, opts.Arch, opts.Machine, effectiveDistro)
	hashes, err := resolve.ComputeAllHashes(dag, opts.Arch, opts.Machine, srcInputs, effectiveDistro)
	if err != nil {
		return err
	}

	requested := make(map[string]bool)
	if len(names) > 0 {
		for _, n := range names {
			requested[n] = true
		}
		order, err = filterBuildOrder(dag, order, names)
		if err != nil {
			return err
		}
	}

	if opts.DryRun {
		return dryRun(w, proj, order, hashes, opts, requested)
	}

	notify := func(unit, status string) {
		if opts.OnEvent != nil {
			opts.OnEvent(BuildEvent{Unit: unit, Status: status})
		}
	}

	for _, name := range order {
		hash := hashes[name]
		unit := proj.LookupUnit(effectiveDistro, name)
		sd := ScopeDir(unit, opts.Arch, opts.Machine)
		forceThis := (opts.Force || opts.Clean) && (len(requested) == 0 || requested[name])
		if !forceThis && !opts.NoCache && cacheValid(proj, opts.ProjectDir, unit, sd, opts.Arch, hash, effectiveDistro) {
			notify(name, "cached")
		} else {
			notify(name, "waiting")
		}
	}

	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	parallel := opts.Parallel
	if parallel <= 0 {
		parallel = DefaultParallel
	}

	sw := &syncWriter{w: w}

	orderSet := make(map[string]bool, len(order))
	for _, name := range order {
		orderSet[name] = true
	}

	indeg := make(map[string]int, len(order))
	for _, name := range order {
		c := 0
		for _, d := range dag.Nodes[name].Deps {
			if orderSet[d] {
				c++
			}
		}
		indeg[name] = c
	}

	var (
		mu             sync.Mutex
		rebuilt        = map[string]bool{}
		started        = map[string]bool{}
		publishedDeb   bool
		imageRefreshed bool
		firstErr       error
		stop           bool
		progress       atomic.Int64
	)
	total := len(order)

	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	var launchReady func()

	worker := func(name string) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		mu.Lock()
		if stop {
			mu.Unlock()
			return
		}
		depRebuilt := false
		for _, d := range dag.Nodes[name].Deps {
			if rebuilt[d] {
				depRebuilt = true
				break
			}
		}
		mu.Unlock()

		unit := proj.LookupUnit(effectiveDistro, name)
		hash := hashes[name]
		sd := ScopeDir(unit, opts.Arch, opts.Machine)

		if err := ctx.Err(); err != nil {
			mu.Lock()
			if firstErr == nil {
				firstErr = fmt.Errorf("build cancelled")
			}
			stop = true
			mu.Unlock()
			return
		}

		forceThis := (opts.Force || opts.Clean) && (len(requested) == 0 || requested[name])

		built := false
		n := progress.Add(1)
		if !forceThis && !opts.NoCache && !depRebuilt &&
			cacheValid(proj, opts.ProjectDir, unit, sd, opts.Arch, hash, effectiveDistro) {
			fmt.Fprintf(sw, "%-20s ⚡ [cached %d/%d units] %s\n", name, n, total, hash[:12])
		} else {
			fmt.Fprintf(sw, "%-20s 🔨 [building %d/%d units]\n", name, n, total)
			notify(name, "building")
			if err := buildOne(ctx, proj, dag, unit, hash, opts, sw); err != nil {
				notify(name, "failed")
				fmt.Fprintf(sw, "%-20s ❌ [failed] %v\n", name, err)
				blocked := blockedUnits(dag, name, order)
				if len(blocked) > 0 {
					fmt.Fprintf(sw, "  the following units depend on %s and cannot be built:\n", name)
					for _, b := range blocked {
						fmt.Fprintf(sw, "    - %s\n", b)
					}
				}
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("building %s: %w", name, err)
				}
				stop = true
				mu.Unlock()
				return
			}
			writeCacheMarker(opts.ProjectDir, sd, name, hash, effectiveDistro)
			fmt.Fprintf(sw, "%-20s ✅ [done] %s\n", name, hash[:12])
			notify(name, "done")
			built = true
		}

		mu.Lock()
		if built {
			rebuilt[name] = true
			if osbstar.IsAptFamily(opts.EffectiveDistro) {
				switch unit.Class {
				case "image":
					imageRefreshed = true
				case "container":
				default:
					publishedDeb = true
				}
			}
		}
		for _, rd := range dag.Nodes[name].Rdeps {
			if orderSet[rd] {
				indeg[rd]--
			}
		}
		launchReady()
		mu.Unlock()
	}

	launchReady = func() {
		if stop {
			return
		}
		for _, name := range order {
			if !started[name] && indeg[name] == 0 {
				started[name] = true
				wg.Add(1)
				go worker(name)
			}
		}
	}

	mu.Lock()
	launchReady()
	mu.Unlock()
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}

	if publishedDeb && !imageRefreshed {
		suite, err := proj.SuiteForDistro(effectiveDistro)
		if err != nil {
			return fmt.Errorf("refresh %s index: %w", effectiveDistro, err)
		}
		if err := repo.GenerateDebianIndex(repo.DebRepoOptions{
			RepoDir:    repo.RepoDistroDir(proj, opts.ProjectDir, effectiveDistro),
			Suite:      suite,
			Components: []string{"main"},
			Arches:     []string{"amd64", "arm64"},
		}); err != nil {
			return fmt.Errorf("refresh %s index: %w", effectiveDistro, err)
		}
	}
	return nil
}

const buildLogTailLines = 50

func reportBuildFailure(w io.Writer, unitName, taskName, logPath string, verbose bool) {
	fmt.Fprintf(w, "  ❌ FAILED: %s task: %s\n", unitName, taskName)
	fmt.Fprintf(w, "  build log: %s\n", logPath)
	if verbose {
		return
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return
	}
	if len(lines) > buildLogTailLines {
		lines = lines[len(lines)-buildLogTailLines:]
		fmt.Fprintf(w, "  ── build.log (last %d lines) ──\n", buildLogTailLines)
	} else {
		fmt.Fprintf(w, "  ── build.log ──\n")
	}
	for _, ln := range lines {
		fmt.Fprintf(w, "  │ %s\n", ln)
	}
	fmt.Fprintf(w, "  ──\n")
}

func buildOne(ctx context.Context, proj *osbstar.Project, dag *resolve.DAG, unit *osbstar.Unit, hash string, opts Options, w io.Writer) (buildErr error) {
	sd := ScopeDir(unit, opts.Arch, opts.Machine)
	distro := opts.EffectiveDistro
	buildDir := UnitBuildDir(opts.ProjectDir, sd, unit.Name, distro)
	EnsureDir(buildDir)

	if IsBuildInProgress(opts.ProjectDir, sd, unit.Name, distro) {
		fmt.Fprintf(w, "  ⏭️  %s: build already in progress, skipping\n", unit.Name)
		return nil
	}

	os.Remove(CacheMarkerPath(opts.ProjectDir, sd, unit.Name, hash, distro))

	lockPath := BuildingLockPath(opts.ProjectDir, sd, unit.Name, distro)
	os.WriteFile(lockPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
	defer os.Remove(lockPath)

	buildStart := time.Now()
	meta := initBuildMeta(buildDir, hash, buildStart)
	WriteMeta(buildDir, meta)
	defer func() {
		now := time.Now()
		meta.Finished = &now
		meta.Duration = now.Sub(buildStart).Seconds()
		meta.DiskBytes = DirSize(buildDir)
		installedRoot := filepath.Join(buildDir, "destdir")
		if unit.Class == "image" {
			installedRoot = filepath.Join(installedRoot, "rootfs")
		}
		meta.InstalledBytes = DirSize(installedRoot)
		cachedState := source.State(meta.SourceState)
		if cachedState == source.StateEmpty {
			cachedState = source.StatePin
		}
		if next := finalizeSourceState(filepath.Join(buildDir, "src"), cachedState); next != source.StateEmpty {
			meta.SourceState = string(next)
		}
		if source.IsDev(source.State(meta.SourceState)) {
			meta.SourceDescribe = source.SrcDescribe(filepath.Join(buildDir, "src"))
		}
		if ctx.Err() != nil {
			meta.Status = "cancelled"
		} else if buildErr != nil {
			meta.Status = "failed"
			meta.Error = buildErr.Error()
		} else {
			meta.Status = "complete"
		}
		WriteMeta(buildDir, meta)
	}()

	outputPath := filepath.Join(buildDir, "executor.log")
	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("creating output log: %w", err)
	}
	defer outputFile.Close()
	w = io.MultiWriter(w, outputFile)

	logPath := filepath.Join(buildDir, "build.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("creating build log: %w", err)
	}
	defer logFile.Close()

	var logW io.Writer
	if opts.Verbose {
		logW = io.MultiWriter(w, logFile)
	} else {
		logW = logFile
	}

	srcDir := filepath.Join(buildDir, "src")
	destDir := filepath.Join(buildDir, "destdir")

	containerImage := resolveContainerImage(proj, unit, opts.Arch, opts.EffectiveDistro)
	containerArch := opts.Arch
	if unit.ContainerArch == "host" {
		containerArch = Arch()
	}

	if opts.Clean {
		if err := removeDirRobust(ctx, srcDir, opts.ProjectDir, containerImage, containerArch); err != nil {
			return fmt.Errorf("removing srcdir: %w", err)
		}
	}

	if err := removeDirRobust(ctx, destDir, opts.ProjectDir, containerImage, containerArch); err != nil {
		return fmt.Errorf("removing destdir: %w", err)
	}
	if err := EnsureDir(destDir); err != nil {
		return fmt.Errorf("creating destdir: %w", err)
	}

	if unit.Source != "" {
		var cachedSourceState string
		if meta := ReadMeta(buildDir); meta != nil {
			cachedSourceState = meta.SourceState
		}
		if _, err := source.Prepare(opts.ProjectDir, sd, opts.EffectiveDistro, unit, cachedSourceState, w); err != nil {
			return fmt.Errorf("preparing source: %w", err)
		}
	} else {
		EnsureDir(srcDir)
	}

	if len(unit.Tasks) == 0 && len(unit.Services) == 0 {
		fmt.Fprintf(w, "  (no tasks for %s class %q)\n", unit.Name, unit.Class)
		return nil
	}

	sysroot := filepath.Join(buildDir, "sysroot")
	if unit.Class != "image" {
		if err := AssembleSysroot(sysroot, dag, unit.Name, opts.ProjectDir, opts.Arch, distro); err != nil {
			return fmt.Errorf("assembling sysroot: %w", err)
		}
	} else if err := EnsureDir(sysroot); err != nil {
		return err
	}
	console := ""
	if m, ok := proj.Machines[opts.Machine]; ok {
		console = m.Console
	}

	env := SysrootEnv("/build/sysroot", opts.Arch)
	env["PREFIX"] = "/usr"
	env["DESTDIR"] = "/build/destdir"
	env["NPROC"] = NProc()
	env["ARCH"] = opts.Arch
	env["MACHINE"] = opts.Machine
	env["DISTRO"] = opts.EffectiveDistro
	env["CONSOLE"] = console
	env["HOME"] = "/tmp"
	env["REPO"] = filepath.Join("/project", repoRelPath(proj, opts.ProjectDir), opts.EffectiveDistro)

	if osbstar.IsAptFamily(opts.EffectiveDistro) {
		suite, serr := proj.SuiteForDistro(opts.EffectiveDistro)
		if serr != nil {
			return fmt.Errorf("resolving %s suite: %w", opts.EffectiveDistro, serr)
		}
		env["SUITE"] = suite
	}

	if opts.Signer != nil {
		env["OSB_KEYS_DIR"] = filepath.Join("/project", repoRelPath(proj, opts.ProjectDir), opts.EffectiveDistro, "keys")
		env["OSB_KEY_NAME"] = opts.Signer.KeyName
	}

	for k, v := range unit.Environment {
		env[k] = v
	}

	hostDir := ""
	if unit.Class == "container" && unit.DefinedIn != "" {
		hostDir = unit.DefinedIn
	}

	var cacheDirs map[string]string
	if len(unit.CacheDirs) > 0 {
		cacheDirs = make(map[string]string, len(unit.CacheDirs))
		for containerPath, subdir := range unit.CacheDirs {
			hostPath := filepath.Join(opts.ProjectDir, "cache", subdir)
			os.MkdirAll(hostPath, 0755)
			cacheDirs[hostPath] = containerPath
		}
	}

	projectName, projectVersion := "", ""
	baseVersion := ""
	if proj != nil {
		projectName = proj.Name
		projectVersion = proj.Version
		baseVersion = proj.BaseVersionForDistro(opts.EffectiveDistro)
	}
	tctxData := BuildTemplateContext(unit, opts.Arch, opts.Machine, console, projectName, projectVersion, opts.EffectiveDistro, baseVersion)

	if unit.Class == "image" && osbstar.IsAptFamily(opts.EffectiveDistro) {
		suite, serr := proj.SuiteForDistro(opts.EffectiveDistro)
		if serr != nil {
			return fmt.Errorf("refresh %s index: %w", opts.EffectiveDistro, serr)
		}
		if err := repo.GenerateDebianIndex(repo.DebRepoOptions{
			RepoDir:    repo.RepoDistroDir(proj, opts.ProjectDir, opts.EffectiveDistro),
			Suite:      suite,
			Components: []string{"main"},
			Arches:     []string{"amd64", "arm64"},
		}); err != nil {
			return fmt.Errorf("refresh %s index: %w", opts.EffectiveDistro, err)
		}
	}

	sandboxArch := opts.Arch
	if unit.ContainerArch == "host" {
		sandboxArch = Arch()
	}

	for ti, t := range unit.Tasks {
		fmt.Fprintf(w, "%-20s [%d/%d] task: %s\n", unit.Name, ti+1, len(unit.Tasks), t.Name)
		fmt.Fprintf(logW, "  task: %s (%d steps)\n", t.Name, len(t.Steps))

		taskContainer := containerImage
		if t.Container != "" {
			taskUnit := *unit
			taskUnit.Container = t.Container
			taskContainer = resolveContainerImage(proj, &taskUnit, opts.Arch, opts.EffectiveDistro)
		}

		for i, step := range t.Steps {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("build cancelled")
			}

			if step.Install != nil {
				fmt.Fprintf(logW, "    [%d/%d] %s\n", i+1, len(t.Steps), installStepLabel(step.Install))
				hostEnv := make(map[string]string, len(env)+3)
				for k, v := range env {
					hostEnv[k] = v
				}
				hostEnv["DESTDIR"] = destDir
				hostEnv["SRCDIR"] = srcDir
				hostEnv["SYSROOT"] = sysroot
				if err := doInstallStep(unit, step.Install, tctxData, hostEnv); err != nil {
					reportBuildFailure(w, unit.Name, t.Name, logPath, opts.Verbose)
					return fmt.Errorf("task %s: %w", t.Name, err)
				}
				continue
			}

			if step.Command != "" {
				fmt.Fprintf(logW, "    [%d/%d] %s\n", i+1, len(t.Steps), step.Command)
				cfg := &SandboxConfig{
					Ctx:        ctx,
					Arch:       sandboxArch,
					Container:  taskContainer,
					Sandbox:    unit.Sandbox,
					Shell:      unit.Shell,
					SrcDir:     srcDir,
					DestDir:    destDir,
					Sysroot:    sysroot,
					Env:        env,
					ProjectDir: opts.ProjectDir,
					HostDir:    hostDir,
					CacheDirs:  cacheDirs,
					Stdout:     logW,
					Stderr:     logW,
				}
				if err := RunInSandbox(cfg, step.Command); err != nil {
					reportBuildFailure(w, unit.Name, t.Name, logPath, opts.Verbose)
					return err
				}
			} else if step.Fn != nil {
				fmt.Fprintf(logW, "    [%d/%d] fn: %s\n", i+1, len(t.Steps), step.Fn.Name())
				cfg := &SandboxConfig{
					Ctx:        ctx,
					Arch:       sandboxArch,
					Container:  taskContainer,
					Sandbox:    unit.Sandbox,
					Shell:      unit.Shell,
					SrcDir:     srcDir,
					DestDir:    destDir,
					Sysroot:    sysroot,
					Env:        env,
					ProjectDir: opts.ProjectDir,
					HostDir:    hostDir,
					CacheDirs:  cacheDirs,
					Stdout:     logW,
					Stderr:     logW,
				}
				thread := NewBuildThread(ctx, cfg, RealExecer{})
				if _, err := starlark.Call(thread, step.Fn, nil, nil); err != nil {
					reportBuildFailure(w, unit.Name, t.Name, logPath, opts.Verbose)
					return fmt.Errorf("task %s: %w", t.Name, err)
				}
			}
		}
	}

	if unit.Class == "image" {
		if err := writeImageSBOM(unit, destDir, opts.EffectiveDistro, w); err != nil {
			fmt.Fprintf(w, "  ⚠️  (warning: SBOM generation failed: %v)\n", err)
		}
		if err := writeImageBmaps(destDir, w); err != nil {
			fmt.Fprintf(w, "  ⚠️  (warning: bmap generation failed: %v)\n", err)
		}
	}

	if unit.Class != "image" && unit.Class != "container" {
		switch {
		case osbstar.IsAptFamily(opts.EffectiveDistro):
			if err := packageDeb(unit, destDir, srcDir, buildDir, opts, proj, w); err != nil {
				return fmt.Errorf("packaging deb: %w", err)
			}
		default:
			if err := packageAPK(unit, destDir, sysroot, srcDir, buildDir, opts, proj, w); err != nil {
				return err
			}
		}

		if err := StageSysroot(destDir, buildDir); err != nil {
			fmt.Fprintf(w, "  ⚠️  (warning: sysroot staging failed: %v)\n", err)
		}
	}

	return nil
}

func writeImageSBOM(unit *osbstar.Unit, destDir, distro string, w io.Writer) error {
	comps, err := sbom.FromRootfs(filepath.Join(destDir, "rootfs"), distro)
	if err != nil {
		return err
	}
	out := filepath.Join(destDir, unit.Name+".sbom.json")
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := sbom.WriteCycloneDX(f, unit.Name, unit.Version, distro, comps); err != nil {
		return err
	}
	fmt.Fprintf(w, "  📋 SBOM: %d packages -> %s\n", len(comps), filepath.Base(out))
	return nil
}

func writeImageBmaps(destDir string, w io.Writer) error {
	imgs, err := filepath.Glob(filepath.Join(destDir, "*.img"))
	if err != nil {
		return err
	}
	for _, img := range imgs {
		if strings.HasSuffix(img, ".run.img") || strings.HasSuffix(img, ".target.img") {
			continue
		}
		mapped, total, err := device.WriteBmap(img, device.BmapPathFor(img))
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(img), err)
		}
		fmt.Fprintf(w, "  🗺️  bmap: %d/%d blocks mapped -> %s\n",
			mapped, total, filepath.Base(device.BmapPathFor(img)))
	}
	return nil
}

func packageAPK(unit *osbstar.Unit, destDir, sysroot, srcDir, buildDir string, opts Options, proj *osbstar.Project, w io.Writer) error {
	archDir := RepoArchDir(unit, opts.Arch)
	var (
		apkPath string
		err     error
	)
	if unit.PassthroughAPK != "" {
		srcAPK := filepath.Join(srcDir, unit.PassthroughAPK)
		apkPath, err = artifact.RepackAPK(unit, srcAPK, filepath.Join(buildDir, "pkg"), opts.Signer)
		if err != nil {
			return fmt.Errorf("repacking upstream apk: %w", err)
		}
		if a, aerr := artifact.ReadAPKArch(srcAPK); aerr == nil && a != "" {
			archDir = a
		}
	} else {
		apkPath, err = artifact.CreateAPK(unit, destDir, sysroot, filepath.Join(buildDir, "pkg"), archDir, opts.ProjectCommit, opts.Signer)
		if err != nil {
			return fmt.Errorf("creating apk: %w", err)
		}
	}
	fmt.Fprintf(w, "  📦 %s\n", filepath.Base(apkPath))

	repoDir := repo.RepoDistroDir(proj, opts.ProjectDir, opts.EffectiveDistro)
	if err := repo.Publish(apkPath, repoDir, archDir, opts.Signer); err != nil {
		return fmt.Errorf("publishing to repo: %w", err)
	}
	return nil
}

func packageDeb(unit *osbstar.Unit, destDir, srcDir, buildDir string, opts Options, proj *osbstar.Project, w io.Writer) error {
	pkgDir := filepath.Join(buildDir, "pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return fmt.Errorf("mkdir pkg: %w", err)
	}

	var debPath string
	if unit.PassthroughDeb != "" {
		src := filepath.Join(srcDir, unit.PassthroughDeb)
		if unit.SHA256 != "" {
			if err := repo.VerifyMirrorSHA256(src, unit.SHA256); err != nil {
				return err
			}
		}
		debPath = filepath.Join(pkgDir, unit.PassthroughDeb)
		if err := copyDebFile(src, debPath); err != nil {
			return fmt.Errorf("copy passthrough deb: %w", err)
		}
	} else {
		debArch := debArchForOsb(opts.Arch)
		fname := fmt.Sprintf("%s_%s_%s.deb", unit.Name, unit.Version, debArch)
		debPath = filepath.Join(pkgDir, fname)

		c := deb.Control{
			Package:      unit.Name,
			Version:      unit.Version,
			Architecture: debArch,
			Maintainer:   "Osb <build@osb.local>",
			Description:  unit.Description,
			Depends:      strings.Join(unit.RuntimeDepsForDistro(opts.EffectiveDistro), ", "),
			Provides:     debProvides(unit.Provides, unit.Version),
		}
		if c.Description == "" {
			c.Description = unit.Name
		}
		if err := deb.MaterializeSystemdServiceSymlinks(destDir, "", unit.Services); err != nil {
			return fmt.Errorf("service symlinks: %w", err)
		}
		if err := deb.BuildDeb(destDir, c, debPath, ""); err != nil {
			return fmt.Errorf("BuildDeb: %w", err)
		}
	}
	fmt.Fprintf(w, "  📦 %s\n", filepath.Base(debPath))

	suite, err := proj.SuiteForDistro(opts.EffectiveDistro)
	if err != nil {
		return fmt.Errorf("packaging deb: %w", err)
	}
	repoDir := repo.RepoDistroDir(proj, opts.ProjectDir, opts.EffectiveDistro)
	publishOpts := repo.DebRepoOptions{
		RepoDir:    repoDir,
		Suite:      suite,
		Components: []string{"main"},
		Arches:     []string{"amd64", "arm64"},
	}
	if err := repo.PublishDeb(debPath, publishOpts, "main"); err != nil {
		return fmt.Errorf("PublishDeb: %w", err)
	}
	return nil
}

func debProvides(provides []string, version string) string {
	if len(provides) == 0 {
		return ""
	}
	out := make([]string, 0, len(provides))
	for _, p := range provides {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(p, "(") {
			out = append(out, p)
			continue
		}
		out = append(out, fmt.Sprintf("%s (= %s)", p, version))
	}
	return strings.Join(out, ", ")
}

func debArchForOsb(osbArch string) string {
	switch osbArch {
	case "x86_64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return osbArch
	}
}

func copyDebFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func filterBuildOrder(dag *resolve.DAG, fullOrder []string, names []string) ([]string, error) {
	needed := make(map[string]bool)
	for _, name := range names {
		if _, ok := dag.Nodes[name]; !ok {
			return nil, fmt.Errorf("unit %q not found", name)
		}
		needed[name] = true
		deps, _ := dag.DepsOf(name)
		for _, d := range deps {
			needed[d] = true
		}
	}

	var filtered []string
	for _, name := range fullOrder {
		if needed[name] {
			filtered = append(filtered, name)
		}
	}
	return filtered, nil
}

func blockedUnits(dag *resolve.DAG, failed string, order []string) []string {
	rdeps, err := dag.RdepsOf(failed)
	if err != nil {
		return nil
	}
	rdepSet := make(map[string]bool, len(rdeps))
	for _, r := range rdeps {
		rdepSet[r] = true
	}
	var blocked []string
	for _, name := range order {
		if rdepSet[name] {
			blocked = append(blocked, name)
		}
	}
	return blocked
}

func dryRun(w io.Writer, proj *osbstar.Project, order []string, hashes map[string]string, opts Options, requested map[string]bool) error {
	fmt.Fprintln(w, "Dry run - would build in this order:")
	for _, name := range order {
		unit := proj.LookupUnit(opts.EffectiveDistro, name)
		sd := ScopeDir(unit, opts.Arch, opts.Machine)
		cached := ""
		forceThis := (opts.Force || opts.Clean) && (len(requested) == 0 || requested[name])
		if !forceThis && cacheValid(proj, opts.ProjectDir, unit, sd, opts.Arch, hashes[name], opts.EffectiveDistro) {
			cached = " [cached, skip]"
		}
		fmt.Fprintf(w, "  %-20s [%s] %s%s\n", name, unit.Class, hashes[name][:12], cached)
	}
	return nil
}

func resolveContainerImage(proj *osbstar.Project, unit *osbstar.Unit, arch, effectiveDistro string) string {
	container := unit.Container
	if container == "" {
		return ""
	}

	if strings.Contains(container, ":") || strings.Contains(container, "/") {
		return container
	}

	distroCtx := unit.Distro
	if distroCtx == "" {
		distroCtx = effectiveDistro
	}

	if resolved := proj.ResolveProvidesForDistro(container, distroCtx); resolved != "" {
		container = resolved
	}

	cu := proj.LookupUnit(distroCtx, container)
	if cu == nil {
		cu = proj.AnyUnit(container)
	}
	if cu != nil {
		imageArch := arch
		if unit.ContainerArch == "host" {
			imageArch = Arch()
		}
		return fmt.Sprintf("osb/%s:%s-%s", container, cu.Version, imageArch)
	}

	return container
}

func removeDirRobust(ctx context.Context, dir, projectDir, image, arch string) error {
	err := os.RemoveAll(dir)
	if err == nil {
		return nil
	}
	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		return nil
	}
	if cerr := chownDirToHost(ctx, dir, projectDir, image, arch); cerr != nil {
		return fmt.Errorf("%w (and ownership recovery failed: %v)", err, cerr)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing after ownership recovery: %w", err)
	}
	return nil
}

func chownDirToHost(ctx context.Context, dir, projectDir, image, arch string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	if image == "" {
		return fmt.Errorf("no container image available for ownership recovery of %s", dir)
	}
	parent := filepath.Dir(dir)
	base := filepath.Base(dir)
	uid := os.Getuid()
	gid := os.Getgid()
	return osb.RunInContainer(osb.ContainerRunConfig{
		Ctx:        ctx,
		Image:      image,
		Arch:       arch,
		Command:    fmt.Sprintf("chown -R %d:%d /__osb_cleanup/%s", uid, gid, base),
		ProjectDir: projectDir,
		Mounts:     []osb.Mount{{Host: parent, Container: "/__osb_cleanup"}},
		NoUser:     true,
		Quiet:      true,
	})
}

func CacheMarkerPath(projectDir, arch, name, hash, distro string) string {
	return filepath.Join(UnitBuildDir(projectDir, arch, name, distro), ".osb-hash")
}

func IsBuildCached(projectDir, arch, name, hash, distro string) bool {
	data, err := os.ReadFile(CacheMarkerPath(projectDir, arch, name, hash, distro))
	if err != nil {
		return false
	}
	return string(data) == hash
}

func cacheValid(proj *osbstar.Project, projectDir string, unit *osbstar.Unit, scopeDir, arch, hash, distro string) bool {
	if !IsBuildCached(projectDir, scopeDir, unit.Name, hash, distro) {
		return false
	}
	if unit.Class == "image" || unit.Class == "container" {
		return true
	}
	repoBase := repo.RepoDistroDir(proj, projectDir, distro)

	if osbstar.IsAptFamily(distro) {
		debName := filepath.Base(unit.PassthroughDeb)
		if debName == "." || debName == "" {
			debName = fmt.Sprintf("%s_%s_%s.deb", unit.Name, unit.Version, debArchForOsb(arch))
		}
		matches, _ := filepath.Glob(filepath.Join(repoBase, "pool", "*", "*", "*", debName))
		return len(matches) > 0
	}

	archDir := RepoArchDir(unit, arch)
	apkName := fmt.Sprintf("%s-%s-r%d.apk", unit.Name, unit.Version, unit.Release)
	if _, err := os.Stat(filepath.Join(repoBase, archDir, apkName)); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(repoBase, "noarch", apkName)); err == nil {
		return true
	}
	return false
}

func BuildingLockPath(projectDir, arch, name, distro string) string {
	return filepath.Join(UnitBuildDir(projectDir, arch, name, distro), ".lock")
}

func IsBuildInProgress(projectDir, arch, name, distro string) bool {
	data, err := os.ReadFile(BuildingLockPath(projectDir, arch, name, distro))
	if err != nil {
		return false
	}
	pid := strings.TrimSpace(string(data))
	_, err = os.Stat(fmt.Sprintf("/proc/%s", pid))
	return err == nil
}

func writeCacheMarker(projectDir, arch, name, hash, distro string) {
	path := CacheMarkerPath(projectDir, arch, name, hash, distro)
	EnsureDir(filepath.Dir(path))
	os.WriteFile(path, []byte(hash), 0644)
}

func readProjectCommit(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func repoRelPath(proj *osbstar.Project, projectDir string) string {
	repoDir := repo.RepoDir(proj, projectDir)
	rel, err := filepath.Rel(projectDir, repoDir)
	if err != nil {
		return "repo"
	}
	return rel
}

func SrcInputsFn(projectDir, arch, machine, distro string) func(u *osbstar.Unit) string {
	return func(u *osbstar.Unit) string {
		sd := ScopeDir(u, arch, machine)
		buildDir := UnitBuildDir(projectDir, sd, u.Name, distro)
		persisted := source.StateEmpty
		if meta := ReadMeta(buildDir); meta != nil {
			persisted = source.State(meta.SourceState)
		}
		if !source.IsDev(persisted) {
			return ""
		}
		srcDir := filepath.Join(buildDir, "src")
		liveState, _ := source.DetectState(srcDir, persisted)
		if !source.IsDev(liveState) {
			liveState = persisted
		}
		return source.SrcHashInputs(srcDir, liveState)
	}
}

func finalizeSourceState(srcDir string, cached source.State) source.State {
	if _, err := os.Stat(filepath.Join(srcDir, ".git")); err != nil {
		return source.StateEmpty
	}
	if source.IsDev(cached) {
		return source.StateDev
	}
	if cached == source.StatePin {
		return source.StatePin
	}
	return source.StateEmpty
}
