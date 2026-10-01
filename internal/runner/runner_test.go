package runner

import (
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

func app(name string, tags ...string) config.Application {
	return config.Application{
		Name:    name,
		Tags:    tags,
		Source:  config.Sources{config.SourceWinget},
		Package: config.Packages{Winget: &config.WingetSpec{ID: "Vendor." + name}},
	}
}

// --tags was the last piece of a field that already appeared in list output,
// the detail pane and the browser's filter but selected nothing.
func TestSelectAppsByTags(t *testing.T) {
	cfg := &config.Config{Applications: []config.Application{
		app("Git", "dev", "core"),
		app("Neovim", "dev", "editor"),
		app("Steam", "gaming"),
		app("jq"),
	}}

	cases := []struct {
		tags []string
		want []string
	}{
		{nil, []string{"Git", "Neovim", "Steam", "jq"}},
		{[]string{"dev"}, []string{"Git", "Neovim"}},
		{[]string{"gaming"}, []string{"Steam"}},
		// Any tag matches, not all of them.
		{[]string{"core", "gaming"}, []string{"Git", "Steam"}},
		{[]string{"DEV"}, []string{"Git", "Neovim"}}, // case-insensitive
		{[]string{"nothing"}, nil},
	}

	for _, c := range cases {
		got, err := selectApps(cfg, "", "", c.tags)
		if err != nil {
			t.Fatalf("selectApps(%v): %v", c.tags, err)
		}
		if len(got) != len(c.want) {
			t.Errorf("--tags %v selected %d applications, want %d", c.tags, len(got), len(c.want))
			continue
		}
		for i := range c.want {
			if got[i].Name != c.want[i] {
				t.Errorf("--tags %v selected %v, want %v", c.tags, names(got), c.want)
				break
			}
		}
	}
}

func names(apps []config.Application) []string {
	out := make([]string, len(apps))
	for i, a := range apps {
		out[i] = a.Name
	}
	return out
}

// A flag that was given wins; an empty flag takes the file's value.
func TestOptionsApplySettings(t *testing.T) {
	settings := &config.Settings{
		InstallDir:  `C:\FromFile`,
		GitHubToken: "file-token",
		AddToPath:   true,
	}

	empty := Options{}
	empty.applySettings(settings)
	if empty.InstallDir != `C:\FromFile` || empty.GitHubToken != "file-token" || !empty.AddToPath {
		t.Errorf("settings not applied to empty options: %+v", empty)
	}

	fromFlags := Options{InstallDir: `C:\FromFlag`, GitHubToken: "flag-token"}
	fromFlags.applySettings(settings)
	if fromFlags.InstallDir != `C:\FromFlag` || fromFlags.GitHubToken != "flag-token" {
		t.Errorf("the file overrode the command line: %+v", fromFlags)
	}

	var noSettings Options
	noSettings.applySettings(nil)
	if noSettings.InstallDir != "" {
		t.Error("a nil settings block changed the options")
	}
}
