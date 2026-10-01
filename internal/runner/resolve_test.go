package runner

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/inventory"
	"github.com/lucasassuncao/apptide/internal/state"
)

// fakeInstaller stands in for a package manager so the fallback chain can be
// exercised without one on the machine.
type fakeInstaller struct {
	name      string
	available bool
	installed bool
	version   string
	installErr
	uninstallErr error
	calls        *[]string
}

// installErr is separate so a zero fakeInstaller means "succeeds".
type installErr struct{ err error }

func (f *fakeInstaller) Name() string      { return f.name }
func (f *fakeInstaller) IsAvailable() bool { return f.available }

func (f *fakeInstaller) Install(ctx context.Context, app config.Application) error {
	if f.calls != nil {
		*f.calls = append(*f.calls, f.name)
	}
	return f.err
}

func (f *fakeInstaller) Uninstall(ctx context.Context, app config.Application) error {
	if f.calls != nil {
		*f.calls = append(*f.calls, "uninstall:"+f.name)
	}
	return f.uninstallErr
}

func (f *fakeInstaller) Check(app config.Application) (bool, string) {
	return f.installed, f.version
}

// withInstallers points resolveInstaller at a fixed table for one test.
func withInstallers(t *testing.T, table map[config.Source]*fakeInstaller) {
	t.Helper()
	prev := resolveInstaller
	resolveInstaller = func(src config.Source, _ installer.Options) (installer.Installer, error) {
		if inst, ok := table[src.Normalize()]; ok {
			return inst, nil
		}
		return nil, fmt.Errorf("unknown source %q", src)
	}
	t.Cleanup(func() { resolveInstaller = prev })
}

func multiSourceApp() config.Application {
	return config.Application{
		Name:   "App",
		Source: config.Sources{config.SourceWinget, config.SourceScoop, config.SourceChocolatey},
		Package: config.Packages{
			Winget:     &config.WingetSpec{ID: "Pub.App"},
			Scoop:      &config.ScoopSpec{ID: "app"},
			Chocolatey: &config.ChocolateySpec{ID: "app"},
		},
	}
}

func TestRunInstallUsesTheFirstSourceThatWorks(t *testing.T) {
	var calls []string
	withInstallers(t, map[config.Source]*fakeInstaller{
		config.SourceWinget: {name: "winget", available: true, calls: &calls},
		config.SourceScoop:  {name: "scoop", available: true, calls: &calls},
	})

	out := RunInstall(context.Background(), multiSourceApp(),
		Resolution{Kind: ResolutionFresh, Source: config.SourceWinget}, installer.Options{})

	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if out.Source != config.SourceWinget {
		t.Errorf("installed via %q, want winget", out.Source)
	}
	if len(calls) != 1 || calls[0] != "winget" {
		t.Errorf("calls = %v, want only winget", calls)
	}
	if len(out.Attempts) != 1 || out.Attempts[0].Result != state.ResultOK {
		t.Errorf("attempts = %+v", out.Attempts)
	}
}

// A source that does not offer the package hands over to the next one, and the
// attempt chain records why.
func TestRunInstallFallsBackWhenNotOffered(t *testing.T) {
	var calls []string
	withInstallers(t, map[config.Source]*fakeInstaller{
		config.SourceWinget: {
			name: "winget", available: true, calls: &calls,
			installErr: installErr{fmt.Errorf("%w: no id", installer.ErrNotOffered)},
		},
		config.SourceScoop:      {name: "scoop", available: true, calls: &calls},
		config.SourceChocolatey: {name: "chocolatey", available: true, calls: &calls},
	})

	out := RunInstall(context.Background(), multiSourceApp(),
		Resolution{Kind: ResolutionFresh, Source: config.SourceWinget}, installer.Options{})

	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if out.Source != config.SourceScoop {
		t.Errorf("installed via %q, want scoop", out.Source)
	}
	if len(calls) != 2 {
		t.Errorf("calls = %v, want winget then scoop", calls)
	}
	if len(out.Attempts) != 2 {
		t.Fatalf("attempts = %+v, want two", out.Attempts)
	}
	if out.Attempts[0].Result != state.ResultNotFound {
		t.Errorf("first attempt recorded as %q, want not_found", out.Attempts[0].Result)
	}
	if out.Attempts[1].Result != state.ResultOK {
		t.Errorf("second attempt recorded as %q, want ok", out.Attempts[1].Result)
	}
}

// A manager that is absent from the machine is recorded as unavailable, not as
// a failed install, and does not stop the chain.
func TestRunInstallSkipsUnavailableManagers(t *testing.T) {
	withInstallers(t, map[config.Source]*fakeInstaller{
		config.SourceWinget:     {name: "winget", available: false},
		config.SourceScoop:      {name: "scoop", available: true},
		config.SourceChocolatey: {name: "chocolatey", available: true},
	})

	out := RunInstall(context.Background(), multiSourceApp(),
		Resolution{Kind: ResolutionFresh, Source: config.SourceWinget}, installer.Options{})

	if out.Source != config.SourceScoop {
		t.Errorf("installed via %q, want scoop", out.Source)
	}
	if out.Attempts[0].Result != state.ResultUnavailable {
		t.Errorf("absent manager recorded as %q, want unavailable", out.Attempts[0].Result)
	}
}

// A genuine install failure also advances, and is recorded distinctly from a
// package the source never offered.
func TestRunInstallRecordsFailureDistinctlyFromNotOffered(t *testing.T) {
	withInstallers(t, map[config.Source]*fakeInstaller{
		config.SourceWinget: {
			name: "winget", available: true,
			installErr: installErr{errors.New("installer returned 1603")},
		},
		config.SourceScoop:      {name: "scoop", available: true},
		config.SourceChocolatey: {name: "chocolatey", available: true},
	})

	out := RunInstall(context.Background(), multiSourceApp(),
		Resolution{Kind: ResolutionFresh, Source: config.SourceWinget}, installer.Options{})

	if out.Attempts[0].Result != state.ResultFailed {
		t.Errorf("a failed install was recorded as %q, want failed", out.Attempts[0].Result)
	}
	if out.Attempts[0].Detail == "" {
		t.Error("the failure detail was dropped")
	}
	if out.Source != config.SourceScoop {
		t.Errorf("installed via %q, want scoop", out.Source)
	}
}

// Cancellation stops the chain: the user asking to stop is not a reason to try
// somewhere else.
func TestRunInstallStopsOnCancellation(t *testing.T) {
	var calls []string
	withInstallers(t, map[config.Source]*fakeInstaller{
		config.SourceWinget: {
			name: "winget", available: true, calls: &calls,
			installErr: installErr{context.Canceled},
		},
		config.SourceScoop:      {name: "scoop", available: true, calls: &calls},
		config.SourceChocolatey: {name: "chocolatey", available: true, calls: &calls},
	})

	out := RunInstall(context.Background(), multiSourceApp(),
		Resolution{Kind: ResolutionFresh, Source: config.SourceWinget}, installer.Options{})

	if !errors.Is(out.Err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", out.Err)
	}
	if len(calls) != 1 {
		t.Errorf("the chain continued after cancellation: %v", calls)
	}
}

// An already-installed package is a success, and the binding is still made.
func TestRunInstallTreatsAlreadyInstalledAsSuccess(t *testing.T) {
	withInstallers(t, map[config.Source]*fakeInstaller{
		config.SourceWinget: {
			name: "winget", available: true,
			installErr: installErr{installer.ErrAlreadyInstalled},
		},
		config.SourceScoop:      {name: "scoop", available: true},
		config.SourceChocolatey: {name: "chocolatey", available: true},
	})

	out := RunInstall(context.Background(), multiSourceApp(),
		Resolution{Kind: ResolutionFresh, Source: config.SourceWinget}, installer.Options{})

	if !errors.Is(out.Err, installer.ErrAlreadyInstalled) {
		t.Errorf("err = %v, want ErrAlreadyInstalled", out.Err)
	}
	if out.Source != config.SourceWinget {
		t.Errorf("source = %q, want winget", out.Source)
	}
	if out.Attempts[0].Result != state.ResultOK {
		t.Errorf("attempt recorded as %q, want ok", out.Attempts[0].Result)
	}
}

// A bound application is not re-resolved: only its own source is tried, even
// when the config lists others.
func TestRunInstallDoesNotWanderWhenBound(t *testing.T) {
	var calls []string
	withInstallers(t, map[config.Source]*fakeInstaller{
		config.SourceWinget: {name: "winget", available: true, calls: &calls},
		config.SourceScoop: {
			name: "scoop", available: true, calls: &calls,
			installErr: installErr{errors.New("boom")},
		},
		config.SourceChocolatey: {name: "chocolatey", available: true, calls: &calls},
	})

	out := RunInstall(context.Background(), multiSourceApp(),
		Resolution{Kind: ResolutionBound, Source: config.SourceScoop}, installer.Options{})

	if out.Err == nil {
		t.Fatal("expected the bound source's failure to be reported")
	}
	if len(calls) != 1 || calls[0] != "scoop" {
		t.Errorf("calls = %v, want only the bound source", calls)
	}
	// A single source reports its own error, not a sentence about a chain.
	if got := out.Err.Error(); got != "boom" {
		t.Errorf("err = %q, want the installer's own message", got)
	}
}

func TestRunInstallReportsWhenEverySourceFails(t *testing.T) {
	withInstallers(t, map[config.Source]*fakeInstaller{
		config.SourceWinget:     {name: "winget", available: true, installErr: installErr{errors.New("a")}},
		config.SourceScoop:      {name: "scoop", available: true, installErr: installErr{errors.New("b")}},
		config.SourceChocolatey: {name: "chocolatey", available: true, installErr: installErr{errors.New("c")}},
	})

	out := RunInstall(context.Background(), multiSourceApp(),
		Resolution{Kind: ResolutionFresh, Source: config.SourceWinget}, installer.Options{})

	if out.Err == nil {
		t.Fatal("expected an error")
	}
	if len(out.Attempts) != 3 {
		t.Errorf("attempts = %d, want one per source", len(out.Attempts))
	}
}

func TestRunUninstallGoesThroughTheBoundSource(t *testing.T) {
	var calls []string
	withInstallers(t, map[config.Source]*fakeInstaller{
		config.SourceWinget: {name: "winget", available: true, calls: &calls},
		config.SourceScoop:  {name: "scoop", available: true, calls: &calls},
	})

	out := RunUninstall(context.Background(), multiSourceApp(),
		Resolution{Kind: ResolutionBound, Source: config.SourceScoop}, installer.Options{})

	if out.Err != nil {
		t.Fatalf("unexpected error: %v", out.Err)
	}
	if len(calls) != 1 || calls[0] != "uninstall:scoop" {
		t.Errorf("calls = %v, want only the bound source", calls)
	}
}

// ── ResolveSource ────────────────────────────────────────────────────────────

// stubProber reports a fixed set of sources as installed.
type stubProber map[config.Source]string

func (p stubProber) Probe(src config.Source, _ config.Application) (bool, string) {
	v, ok := p[src.Normalize()]
	return ok, v
}

func TestResolveSourcePrefersTheRecordedBinding(t *testing.T) {
	st := state.New("")
	st.Bind("App", state.Entry{InstalledVia: string(config.SourceChocolatey)})

	// Even with winget reporting the app installed, the binding wins: the
	// preference order applies to the first installation only.
	res := ResolveSource(multiSourceApp(), st, stubProber{config.SourceWinget: "1.0"})

	if res.Kind != ResolutionBound {
		t.Errorf("kind = %v, want ResolutionBound", res.Kind)
	}
	if res.Source != config.SourceChocolatey {
		t.Errorf("source = %q, want chocolatey", res.Source)
	}
}

func TestResolveSourceDetectsASingleInstall(t *testing.T) {
	res := ResolveSource(multiSourceApp(), state.New(""), stubProber{config.SourceScoop: "2.0"})

	if res.Kind != ResolutionDetected {
		t.Errorf("kind = %v, want ResolutionDetected", res.Kind)
	}
	if res.Source != config.SourceScoop {
		t.Errorf("source = %q, want scoop", res.Source)
	}
}

// Two managers each claiming the application is a real problem on the machine,
// so it is surfaced instead of silently picking one.
func TestResolveSourceReportsAConflict(t *testing.T) {
	res := ResolveSource(multiSourceApp(), state.New(""), stubProber{
		config.SourceWinget: "1.0",
		config.SourceScoop:  "2.0",
	})

	if res.Kind != ResolutionConflict {
		t.Fatalf("kind = %v, want ResolutionConflict", res.Kind)
	}
	if len(res.Conflicts) != 2 {
		t.Errorf("conflicts = %v, want both sources", res.Conflicts)
	}
}

func TestResolveSourceFallsBackToPreferenceOrder(t *testing.T) {
	res := ResolveSource(multiSourceApp(), state.New(""), stubProber{})

	if res.Kind != ResolutionFresh {
		t.Errorf("kind = %v, want ResolutionFresh", res.Kind)
	}
	if res.Source != config.SourceWinget {
		t.Errorf("source = %q, want the first preference", res.Source)
	}
}

func TestResolveSourceWithNoSources(t *testing.T) {
	app := config.Application{Name: "App"}
	if res := ResolveSource(app, state.New(""), stubProber{}); res.Kind != ResolutionNoSource {
		t.Errorf("kind = %v, want ResolutionNoSource", res.Kind)
	}
}

// ── RecordState ──────────────────────────────────────────────────────────────

func TestRecordStateBindsTheSourceThatWorked(t *testing.T) {
	st := state.New("")
	out := Outcome{Source: config.SourceScoop}

	RecordState(st, multiSourceApp(), out, "3.1", config.ActionInstall)

	e, ok := st.Get("App")
	if !ok {
		t.Fatal("nothing was recorded")
	}
	if e.InstalledVia != string(config.SourceScoop) {
		t.Errorf("bound to %q, want scoop", e.InstalledVia)
	}
	if e.Version != "3.1" {
		t.Errorf("version = %q, want 3.1", e.Version)
	}
	if e.PackageID != "app" {
		t.Errorf("package id = %q, want the scoop id", e.PackageID)
	}
}

func TestRecordStateClearsTheBindingOnUninstall(t *testing.T) {
	st := state.New("")
	st.Bind("App", state.Entry{InstalledVia: string(config.SourceScoop)})

	RecordState(st, multiSourceApp(), Outcome{Source: config.SourceScoop}, "", config.ActionUninstall)

	if _, ok := st.Get("App"); ok {
		t.Error("the binding survived an uninstall")
	}
}

// The inventory prober answers from one snapshot instead of launching a process
// per application per source, which is what install and verify used to do.
func TestCatalogProberAnswersFromTheSnapshot(t *testing.T) {
	snap := inventory.NewSnapshot(map[config.Source][]inventory.Entry{
		config.SourceWinget: {{ID: "Publisher.App", Version: "1.2.3"}},
		config.SourceScoop:  {},
	})
	probe := NewInventoryProber(snap, installer.Options{})

	app := config.Application{
		Name:   "App",
		Source: config.Sources{config.SourceWinget},
		Package: config.Packages{
			Winget: &config.WingetSpec{ID: "Publisher.App"},
			Scoop:  &config.ScoopSpec{ID: "app"},
		},
	}

	installed, version := probe.Probe(config.SourceWinget, app)
	if !installed || version != "1.2.3" {
		t.Errorf("winget probe = (%v, %q), want (true, \"1.2.3\")", installed, version)
	}

	if installed, _ := probe.Probe(config.SourceScoop, app); installed {
		t.Error("scoop reported the app as installed when its listing is empty")
	}
}

// Scoop keeps failed installs in its listing. Reporting those as installed
// would show a working program where the binary is missing.
func TestCatalogProberTreatsFailedInstallsAsAbsent(t *testing.T) {
	snap := inventory.NewSnapshot(map[config.Source][]inventory.Entry{
		config.SourceScoop: {{ID: "broken", Version: "", Failed: true}},
	})
	probe := NewInventoryProber(snap, installer.Options{})

	app := config.Application{
		Name:    "Broken",
		Source:  config.Sources{config.SourceScoop},
		Package: config.Packages{Scoop: &config.ScoopSpec{ID: "broken"}},
	}

	if installed, _ := probe.Probe(config.SourceScoop, app); installed {
		t.Error("a failed scoop install was reported as installed")
	}
}

// An application with no id for a source cannot be installed through it, and
// must not be looked up under an empty key.
func TestCatalogProberIgnoresSourcesWithNoID(t *testing.T) {
	snap := inventory.NewSnapshot(map[config.Source][]inventory.Entry{
		config.SourceWinget: {{ID: "", Version: "9.9"}},
	})
	probe := NewInventoryProber(snap, installer.Options{})

	app := config.Application{Name: "App", Source: config.Sources{config.SourceWinget}}
	if installed, _ := probe.Probe(config.SourceWinget, app); installed {
		t.Error("an application with no winget id matched an empty-id entry")
	}
}
