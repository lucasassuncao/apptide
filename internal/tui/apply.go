package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/runner"
	"github.com/lucasassuncao/apptide/internal/state"
)

// queueItem is one pending change, as shown in the confirmation dialog.
type queueItem struct {
	app    config.Application
	res    runner.Resolution
	action config.Action
	// verb is what the confirmation shows: install, upgrade or remove.
	verb string
}

// applyEvent is emitted while the queue runs, so the activity pane can show
// the fallback chain as it happens instead of only its outcome.
type applyEvent struct {
	name     string
	verb     string // install, upgrade or remove — without it the log cannot be read
	source   config.Source
	kind     eventKind
	detail   string
	version  string
	finished bool
}

type eventKind uint8

const (
	eventStart eventKind = iota
	eventAttempt
	eventOK
	eventNoop
	eventFail
)

// startApply runs the queue in the background, publishing events on ch.
// State is saved once at the end: a partial run still records what happened.
func startApply(ctx context.Context, items []queueItem, opts installer.Options, st *state.State, dryRun bool, ch chan<- applyEvent) {
	go func() {
		defer close(ch)

		for _, item := range items {
			ch <- applyEvent{name: item.app.Name, verb: item.verb, kind: eventStart, source: item.res.Source}

			if dryRun {
				ch <- applyEvent{
					name: item.app.Name, verb: item.verb, kind: eventNoop, source: item.res.Source,
					detail: "dry-run: would " + item.verb,
				}
				continue
			}

			if cancelled := applyOne(ctx, item, opts, st, ch); cancelled {
				return
			}
		}

		if !dryRun {
			if err := saveState(st); err != nil {
				ch <- applyEvent{kind: eventFail, detail: "saving state: " + err.Error()}
			}
		}
		ch <- applyEvent{finished: true}
	}()
}

// applyOne runs a single queued change and publishes what happened. It reports
// whether the run was cancelled, which stops the whole queue: the user asking
// to stop is not a reason to move on to the next application.
func applyOne(ctx context.Context, item queueItem, opts installer.Options, st *state.State, ch chan<- applyEvent) (cancelled bool) {
	if pre := item.app.Pre(item.action); pre != "" {
		if err := runner.RunHook(ctx, pre); err != nil {
			ch <- applyEvent{name: item.app.Name, verb: item.verb, kind: eventFail, detail: "pre-hook: " + err.Error()}
			return false
		}
	}

	var out runner.Outcome
	if item.action == config.ActionUninstall {
		out = runner.RunUninstall(ctx, item.app, item.res, opts)
	} else {
		out = runner.RunInstall(ctx, item.app, item.res, opts)
	}

	// Replay the attempts so a fallback is visible, not just its result.
	for _, a := range out.Attempts {
		if a.Result == state.ResultOK {
			continue
		}
		ch <- applyEvent{
			name:   item.app.Name,
			source: config.Source(a.Source),
			kind:   eventAttempt,
			detail: string(a.Result),
		}
	}

	switch {
	case out.Err == nil:
		reportSuccess(ctx, item, out, opts, st, ch)

	case errors.Is(out.Err, installer.ErrAlreadyInstalled):
		// Ask what is actually installed rather than binding an empty string,
		// which used to erase the version already on record.
		runner.RecordState(st, item.app, out, currentVersion(item.app, out.Source, opts), item.action)
		ch <- applyEvent{
			name: item.app.Name, verb: item.verb, source: out.Source,
			kind: eventNoop, detail: noopReason(item.app),
		}

	case errors.Is(out.Err, context.Canceled):
		ch <- applyEvent{name: item.app.Name, verb: item.verb, source: out.Source, kind: eventFail, detail: "cancelled"}
		return true

	default:
		ch <- applyEvent{name: item.app.Name, verb: item.verb, source: out.Source, kind: eventFail, detail: out.Err.Error()}
	}
	return false
}

// reportSuccess records the binding, runs the post hook and publishes the
// result. A failing post hook is a warning on an otherwise successful change,
// not a failure of the change itself.
func reportSuccess(ctx context.Context, item queueItem, out runner.Outcome, opts installer.Options, st *state.State, ch chan<- applyEvent) {
	version := currentVersion(item.app, out.Source, opts)
	runner.RecordState(st, item.app, out, version, item.action)

	ev := applyEvent{
		name: item.app.Name, verb: item.verb, source: out.Source,
		kind: eventOK, version: version,
	}

	// Hooks named for install must not fire on a removal.
	if post := item.app.Post(item.action); post != "" {
		if err := runner.RunHook(ctx, post); err != nil {
			ev.detail = "post-hook failed: " + err.Error()
		}
	}
	ch <- ev
}

// stateLockTimeout bounds the wait for another apptide process to finish
// writing the state file.
const stateLockTimeout = 10 * time.Second

// saveState persists the state under the cross-process lock.
//
// The browser is the case the lock was written for: an install running in one
// terminal and a TUI in another would otherwise overwrite each other's
// bindings, whichever saved last. The lock is taken per save rather than for
// the session, because a browser left open all afternoon must not block an
// install.
func saveState(st *state.State) error {
	unlock, err := state.Lock(st.Path(), stateLockTimeout)
	if err != nil {
		return err
	}
	defer unlock()
	return st.Save()
}

// waitForEvent turns the next event on ch into a tea.Msg.
func waitForEvent(ch <-chan applyEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return applyEvent{finished: true}
		}
		return ev
	}
}

// currentVersion asks a source which version it now has installed.
func currentVersion(app config.Application, src config.Source, opts installer.Options) string {
	if src == "" {
		return ""
	}
	inst, err := installer.Resolve(src, opts)
	if err != nil || !inst.IsAvailable() {
		return ""
	}
	_, version := inst.Check(app)
	return version
}

// noopReason explains why nothing happened.
//
// "already up to date" is wrong for a pinned application: it is at the version
// the file asks for, which says nothing about whether a newer one exists. The
// two cases deserve different words, and the Upgradable tab has the numbers.
func noopReason(app config.Application) string {
	switch {
	case app.SkipUpgrade:
		return "already installed — upgrades disabled by skip_upgrade"
	case app.Version != "" && !strings.EqualFold(app.Version, "latest"):
		return "declared version " + app.Version + " already installed"
	default:
		return "already up to date"
	}
}

// past turns the queued verb into what to report once it is done, so the log
// says what happened rather than leaving the reader to infer it from a tick.
func past(verb string) string {
	switch verb {
	case "remove":
		return "removed"
	case "upgrade":
		return "upgraded"
	case "install":
		return "installed"
	default:
		return verb
	}
}

// render turns an event into an activity line.
//
// Every line names the action. Without it an install and an uninstall of the
// same application produced identical output — "▸ adb (scoop)" then "✓ adb" —
// and the log could not be read back.
func (e applyEvent) render() string {
	switch e.kind {
	case eventStart:
		verb := e.verb
		if verb == "" {
			verb = "processing"
		}
		return fmt.Sprintf("%s %s %s %s",
			stCyan.Render("▸"), stCyan.Render(verb), e.name, stDim.Render("("+string(e.source)+")"))

	case eventAttempt:
		return fmt.Sprintf("  %s %s %s", stDim.Render("└─"), stDim.Render(string(e.source)), stYellow.Render(e.detail))

	case eventOK:
		line := fmt.Sprintf("%s %s %s", stGreen.Render("✓"), stGreen.Render(past(e.verb)), e.name)
		// A removal has no version to report, and the previous one would be
		// misleading.
		if e.version != "" && e.verb != "remove" {
			line += " " + stDim.Render(e.version)
		}
		if e.source != "" {
			line += " " + stDim.Render("("+string(e.source)+")")
		}
		if e.detail != "" {
			line += "  " + stYellow.Render(e.detail)
		}
		return line

	case eventNoop:
		return fmt.Sprintf("%s %s %s", stDim.Render("·"), e.name, stDim.Render(e.detail))

	case eventFail:
		verb := e.verb
		if verb == "" {
			verb = "failed"
		}
		return fmt.Sprintf("%s %s %s %s", stRed.Render("✗"), stRed.Render(verb), e.name, stRed.Render(e.detail))

	default:
		return ""
	}
}
