package tui

import (
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

// "already up to date" is the wrong sentence for a pinned application: it sits
// where the file asks, which says nothing about newer releases.
func TestNoopReasonDistinguishesPinnedFromCurrent(t *testing.T) {
	cases := []struct {
		app  config.Application
		want string
	}{
		{config.Application{Name: "Git"}, "already up to date"},
		{config.Application{Name: "Git", Version: "latest"}, "already up to date"},
		{config.Application{Name: "Node.js", Version: "20.11.1"}, "declared version 20.11.1 already installed"},
		{config.Application{Name: "Node.js", SkipUpgrade: true}, "already installed — upgrades disabled by skip_upgrade"},
	}
	for _, c := range cases {
		if got := noopReason(c.app); got != c.want {
			t.Errorf("noopReason(%+v) = %q, want %q", c.app, got, c.want)
		}
	}
}
