package inventory

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

// Managers disagree on case ("Git.Git" against "git.git"), and the config
// may spell a source "choco": both must find the same entry.
func TestLookupIgnoresCaseAndSourceAlias(t *testing.T) {
	snap := NewSnapshot(map[config.Source][]Entry{
		config.SourceChocolatey: {{ID: "Git", Version: "2.51.0"}},
	})

	e, ok := snap.Lookup("choco", "git")
	if !ok || e.Version != "2.51.0" {
		t.Errorf("Lookup(choco, git) = (%+v, %v), want version 2.51.0", e, ok)
	}
	if _, ok := snap.Lookup(config.SourceChocolatey, "vim"); ok {
		t.Error("found a package the listing does not have")
	}
	if _, ok := snap.Lookup(config.SourceScoop, "git"); ok {
		t.Error("found a package under a source that was never queried")
	}
}

// A manager with nothing installed still answered; one that was never
// reached did not. The UI tells the two apart.
func TestAvailableSeparatesEmptyFromUnreachable(t *testing.T) {
	snap := NewSnapshot(map[config.Source][]Entry{
		config.SourceScoop:  {},
		config.SourceWinget: {{ID: "A"}, {ID: "B"}},
	})

	if !snap.Available(config.SourceScoop) {
		t.Error("an empty listing counted as unreachable")
	}
	if snap.Available(config.SourceChocolatey) {
		t.Error("a source that was never queried counted as reachable")
	}
	if got := snap.Count(config.SourceWinget); got != 2 {
		t.Errorf("Count(winget) = %d, want 2", got)
	}
	if got := len(snap.All(config.SourceWinget)); got != 2 {
		t.Errorf("All(winget) has %d entries, want 2", got)
	}
}

func TestSourcesFollowCanonicalOrder(t *testing.T) {
	snap := NewSnapshot(map[config.Source][]Entry{
		config.SourceScoop:  {},
		config.SourceWinget: {},
	})

	var want []config.Source
	for _, src := range config.AllSources() {
		if src == config.SourceWinget || src == config.SourceScoop {
			want = append(want, src)
		}
	}
	if got := snap.Sources(); !slices.Equal(got, want) {
		t.Errorf("Sources() = %v, want %v", got, want)
	}
}

// One manager failing must not hide what the others answered.
func TestQueryAllKeepsErrorsPerSource(t *testing.T) {
	boom := errors.New("boom")
	bySource, errs := queryAll(context.Background(), map[config.Source]func(context.Context) ([]Entry, error){
		config.SourceWinget: func(context.Context) ([]Entry, error) { return []Entry{{ID: "Git.Git"}}, nil },
		config.SourceScoop:  func(context.Context) ([]Entry, error) { return nil, boom },
	}, func(e Entry) string { return e.ID })

	if _, ok := bySource[config.SourceWinget]["git.git"]; !ok {
		t.Error("winget's listing is missing or not keyed by lowercased id")
	}
	if !errors.Is(errs[config.SourceScoop], boom) {
		t.Errorf("scoop error = %v, want boom", errs[config.SourceScoop])
	}
	if _, ok := bySource[config.SourceScoop]; ok {
		t.Error("a failed source was indexed as if it had answered")
	}
}

// A machine without the managers is the normal case, not a failure.
func TestInstalledWithNoManagersReportsThemMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	snap := Installed(context.Background())
	if got := snap.Sources(); len(got) != 0 {
		t.Errorf("Sources() = %v, want none", got)
	}
	for _, src := range []config.Source{config.SourceWinget, config.SourceScoop, config.SourceChocolatey} {
		if err := snap.Err(src); !errors.Is(err, ErrManagerMissing) {
			t.Errorf("Err(%s) = %v, want ErrManagerMissing", src, err)
		}
	}
}
