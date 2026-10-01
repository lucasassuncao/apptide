package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/inventory"
	"github.com/lucasassuncao/apptide/internal/state"
)

// ResolutionKind is how a source was chosen for an application.
type ResolutionKind uint8

const (
	// ResolutionBound: the state file records which source installed it.
	ResolutionBound ResolutionKind = iota
	// ResolutionDetected: no binding, but exactly one source reports it installed.
	ResolutionDetected
	// ResolutionFresh: nothing installed; the preference order applies.
	ResolutionFresh
	// ResolutionConflict: more than one source reports it installed.
	ResolutionConflict
	// ResolutionNoSource: the application declares no usable source.
	ResolutionNoSource
)

// Resolution is the outcome of deciding which source manages an application.
type Resolution struct {
	Kind      ResolutionKind
	Source    config.Source
	Conflicts []config.Source
}

// Prober answers whether a source has an application installed. It exists so
// the TUI can answer from a single inventory snapshot instead of launching one
// process per application, which would take minutes for a full config.
type Prober interface {
	Probe(src config.Source, app config.Application) (installed bool, version string)
}

// installerProber asks each installer directly: one process per application
// per source. It is the fallback, not the default.
type installerProber struct{ opts installer.Options }

func (p installerProber) Probe(src config.Source, app config.Application) (bool, string) {
	inst, err := installer.Resolve(src, p.opts)
	if err != nil || !inst.IsAvailable() {
		return false, ""
	}
	return inst.Check(app)
}

// InventoryProber answers from a single inventory snapshot.
//
// Resolving sixty applications across four sources the slow way is up to two
// hundred and forty process launches before anything is installed, all to
// answer a question three calls to the managers already answer. The github
// source has no inventory, so it falls through to a filesystem check.
type InventoryProber struct {
	snap     *inventory.Snapshot
	fallback Prober
}

// NewInventoryProber builds a prober backed by snap, falling back to the
// installers for sources the inventory cannot answer for.
func NewInventoryProber(snap *inventory.Snapshot, opts installer.Options) InventoryProber {
	return InventoryProber{snap: snap, fallback: installerProber{opts}}
}

func (p InventoryProber) Probe(src config.Source, app config.Application) (bool, string) {
	if p.snap == nil || src.Normalize() == config.SourceGitHub {
		return p.fallback.Probe(src, app)
	}
	id, ok := app.Package.ID(src)
	if !ok || id == "" {
		return false, ""
	}
	e, found := p.snap.Lookup(src, id)
	if e.Failed {
		// The manager kept a record of a failed install. Calling that
		// "installed" would show a green dot over a program that is not there.
		return false, ""
	}
	return found, e.Version
}

// ResolveSource decides which source manages an application right now.
//
// A recorded binding always wins: the preference order in `source:` applies to
// the first installation only. Re-deciding it on every run would uninstall and
// reinstall an application because a third-party catalog gained the package,
// which is never what the user asked for.
func ResolveSource(app config.Application, st *state.State, probe Prober) Resolution {
	if len(app.Source) == 0 {
		return Resolution{Kind: ResolutionNoSource}
	}

	if e, ok := st.Get(app.Name); ok && e.InstalledVia != "" {
		return Resolution{Kind: ResolutionBound, Source: config.Source(e.InstalledVia)}
	}

	var found []config.Source
	for _, src := range app.Source {
		if installed, _ := probe.Probe(src, app); installed {
			found = append(found, src)
		}
	}

	switch len(found) {
	case 0:
		return Resolution{Kind: ResolutionFresh, Source: app.Source[0]}
	case 1:
		return Resolution{Kind: ResolutionDetected, Source: found[0]}
	default:
		// Two managers each believe they own this application. Picking one
		// silently would hide a real problem on the machine.
		return Resolution{Kind: ResolutionConflict, Source: found[0], Conflicts: found}
	}
}

// Outcome is the result of running an application through one or more sources.
type Outcome struct {
	Source   config.Source
	Err      error
	Attempts []state.Attempt
}

// resolveInstaller is installer.Resolve behind a variable so tests can supply
// a fake. The fallback chain is the most consequential logic here and it
// cannot otherwise be exercised without four package managers on the machine.
var resolveInstaller = installer.Resolve

// RunInstall installs app, walking the preference list when nothing is bound.
//
// Every kind of failure advances to the next source: a manager that does not
// offer the package, one that is not on the machine, and one that tried and
// failed. Listing several sources is a statement that any of them is
// acceptable, so falling through is the declared intent rather than a
// substitution — and the attempt chain is recorded and shown, so which one
// ended up installing is never hidden.
//
// Cancellation is the exception: it stops the chain, because the user asking
// to stop is not a reason to try somewhere else.
func RunInstall(ctx context.Context, app config.Application, res Resolution, opts installer.Options) Outcome {
	var out Outcome

	candidates := []config.Source{res.Source}
	if res.Kind == ResolutionFresh {
		candidates = app.Source
	}

	var lastErr error
	var lastSrc config.Source

	for _, src := range candidates {
		inst, err := resolveInstaller(src, opts)
		if err != nil {
			out.Attempts = append(out.Attempts, state.Attempt{
				Source: string(src), Result: state.ResultUnavailable, Detail: err.Error(),
			})
			lastErr, lastSrc = err, src
			continue
		}
		if !inst.IsAvailable() {
			detail := inst.Name() + " not found in PATH"
			out.Attempts = append(out.Attempts, state.Attempt{
				Source: string(src), Result: state.ResultUnavailable, Detail: detail,
			})
			lastErr, lastSrc = errors.New(detail), src
			continue
		}

		err = inst.Install(ctx, app)

		switch {
		case err == nil:
			out.Attempts = append(out.Attempts, state.Attempt{Source: string(src), Result: state.ResultOK})
			out.Source = src
			return out

		case errors.Is(err, installer.ErrAlreadyInstalled):
			out.Attempts = append(out.Attempts, state.Attempt{Source: string(src), Result: state.ResultOK})
			out.Source, out.Err = src, err
			return out

		case errors.Is(err, context.Canceled):
			out.Attempts = append(out.Attempts, state.Attempt{
				Source: string(src), Result: state.ResultFailed, Detail: "cancelled",
			})
			out.Source, out.Err = src, err
			return out

		case errors.Is(err, installer.ErrNotOffered):
			out.Attempts = append(out.Attempts, state.Attempt{
				Source: string(src), Result: state.ResultNotFound, Detail: err.Error(),
			})
			lastErr, lastSrc = err, src

		default:
			out.Attempts = append(out.Attempts, state.Attempt{
				Source: string(src), Result: state.ResultFailed, Detail: err.Error(),
			})
			lastErr, lastSrc = err, src
		}
	}

	out.Source = lastSrc
	if lastErr != nil && len(candidates) == 1 {
		// A single source: report its own error rather than wrapping it in a
		// sentence about a chain that never existed.
		out.Err = lastErr
		return out
	}
	out.Err = fmt.Errorf("no source could install %q (tried %s; last error: %v)",
		app.Name, app.Source, lastErr)
	return out
}

// RunUninstall removes app through the source that installed it.
func RunUninstall(ctx context.Context, app config.Application, res Resolution, opts installer.Options) Outcome {
	out := Outcome{Source: res.Source}

	inst, err := resolveInstaller(res.Source, opts)
	if err != nil {
		out.Err = err
		return out
	}
	if !inst.IsAvailable() {
		out.Err = fmt.Errorf("%s not found in PATH", inst.Name())
		return out
	}
	out.Err = inst.Uninstall(ctx, app)
	return out
}

// RecordState binds the application to the source that handled it, or clears
// the binding after an uninstall.
func RecordState(st *state.State, app config.Application, out Outcome, version string, action config.Action) {
	if st == nil || out.Source == "" {
		return
	}
	if action == config.ActionUninstall {
		st.Remove(app.Name)
		return
	}
	id, _ := app.Package.ID(out.Source)
	st.Bind(app.Name, state.Entry{
		InstalledVia: string(out.Source),
		PackageID:    id,
		Version:      version,
		Attempts:     out.Attempts,
	})
}
