package elevation

import (
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

func TestNeeded(t *testing.T) {
	cases := []struct {
		name string
		app  config.Application
		src  config.Source
		want bool
	}{
		{"chocolatey is machine-wide", config.Application{}, config.SourceChocolatey, true},
		{"choco alias", config.Application{}, "choco", true},
		{"winget defaults to user", config.Application{
			Package: config.Packages{Winget: &config.WingetSpec{ID: "A.B"}},
		}, config.SourceWinget, false},
		{"winget machine scope", config.Application{
			Package: config.Packages{Winget: &config.WingetSpec{ID: "A.B", Scope: "machine"}},
		}, config.SourceWinget, true},
		{"scoop is per user", config.Application{
			Package: config.Packages{Scoop: &config.ScoopSpec{ID: "a"}},
		}, config.SourceScoop, false},
		{"scoop global", config.Application{
			Package: config.Packages{Scoop: &config.ScoopSpec{ID: "a", Global: true}},
		}, config.SourceScoop, true},
		{"github binary", config.Application{
			Package: config.Packages{GitHub: &config.GitHubSpec{ID: "o/r"}},
		}, config.SourceGitHub, false},
		{"github installer", config.Application{
			Package: config.Packages{GitHub: &config.GitHubSpec{ID: "o/r", RunInstaller: true}},
		}, config.SourceGitHub, true},
		// The declaration wins over the guess for a source that needs nothing.
		{"declared requires_admin", config.Application{RequiresAdmin: true}, config.SourceScoop, true},
	}

	for _, c := range cases {
		got, why := Needed(c.app, c.src)
		if got != c.want {
			t.Errorf("%s: Needed = %v, want %v", c.name, got, c.want)
		}
		if got && why == "" {
			t.Errorf("%s: needs elevation but gives no reason", c.name)
		}
	}
}
