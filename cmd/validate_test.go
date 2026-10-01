package cmd

import (
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

func wingetApp(name, id string) config.Application {
	return config.Application{
		Name:    name,
		Source:  config.Sources{config.SourceWinget},
		Package: config.Packages{Winget: &config.WingetSpec{ID: id}},
	}
}

func TestValidateApplication(t *testing.T) {
	cases := []struct {
		name  string
		app   func() config.Application
		field string
		sev   severity
	}{
		{"missing name", func() config.Application { return wingetApp("", "A.B") }, "name", sevError},
		{"bad action", func() config.Application {
			a := wingetApp("A", "A.B")
			a.Action = "reinstall"
			return a
		}, "action", sevError},
		{"no source", func() config.Application { return config.Application{Name: "A"} }, "source", sevError},
		{"repeated source", func() config.Application {
			a := wingetApp("A", "A.B")
			a.Source = config.Sources{config.SourceWinget, config.SourceWinget}
			return a
		}, "source", sevError},
		{"missing id", func() config.Application { return wingetApp("A", "") }, "package.winget.id", sevError},
		{"unused block", func() config.Application {
			a := wingetApp("A", "A.B")
			a.Package.Scoop = &config.ScoopSpec{ID: "a"}
			return a
		}, "package.scoop", sevWarning},
		{"pinned under scoop", func() config.Application {
			return config.Application{
				Name: "A", Version: "1.2.3",
				Source:  config.Sources{config.SourceScoop},
				Package: config.Packages{Scoop: &config.ScoopSpec{ID: "a"}},
			}
		}, "version", sevWarning},
		{"github uninstall", func() config.Application {
			return config.Application{
				Name: "A", Action: config.ActionUninstall,
				Source:  config.Sources{config.SourceGitHub},
				Package: config.Packages{GitHub: &config.GitHubSpec{ID: "o/r"}},
			}
		}, "action", sevError},
		{"github id without owner", func() config.Application {
			return config.Application{
				Name:    "A",
				Source:  config.Sources{config.SourceGitHub},
				Package: config.Packages{GitHub: &config.GitHubSpec{ID: "repo"}},
			}
		}, "package.github.id", sevError},
		{"github args without installer", func() config.Application {
			return config.Application{
				Name:    "A",
				Source:  config.Sources{config.SourceGitHub},
				Package: config.Packages{GitHub: &config.GitHubSpec{ID: "o/r", Args: []string{"/S"}}},
			}
		}, "package.github.args", sevWarning},
	}

	for _, c := range cases {
		issues := validateApplication(c.app())
		found := false
		for _, i := range issues {
			if i.field == c.field && i.severity == c.sev {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no issue on %q with severity %d, got %+v", c.name, c.field, c.sev, issues)
		}
	}
}

func TestValidateApplicationAcceptsAValidEntry(t *testing.T) {
	if issues := validateApplication(wingetApp("Git", "Git.Git")); len(issues) != 0 {
		t.Errorf("a valid application reported %+v", issues)
	}
}

// Warnings describe fields that are ignored, not a broken config: they are
// printed, but validate still succeeds.
func TestRunValidateFailsOnErrorsOnly(t *testing.T) {
	warnOnly := useConfig(t, `schema_version: 2
applications:
  - name: "A"
    source: winget
    package:
      winget:
        id: A.B
      scoop:
        id: a
`)
	captureCmdStdout(t, func() {
		if err := runValidate(warnOnly); err != nil {
			t.Errorf("warnings failed validate: %v", err)
		}
	})

	withError := useConfig(t, `schema_version: 2
applications:
  - name: "A"
    source: winget
`)
	captureCmdStdout(t, func() {
		if err := runValidate(withError); err == nil {
			t.Error("a missing package id passed validate")
		}
	})
}
