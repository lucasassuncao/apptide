package cmd

import (
	"fmt"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/yedit/spec"
	"github.com/lucasassuncao/yedit/validate"
	"gopkg.in/yaml.v3"
)

// AppTideValidators is the rule set the edit command enforces on save.
//
// Per-field constraints (required, allowed values, patterns, counts) are
// declared once in internal/config/metadata.go and enforced by the
// FromMetadata family, so the hint panel and the save-time rules cannot
// disagree. Only cross-field rules live here.
var AppTideValidators = []spec.Validator{
	validate.RequiredFromMetadata(),
	validate.OneOfFromMetadata(),
	validate.PatternFromMetadata(),
	validate.CountFromMetadata(),
	validate.UniqueFromMetadata(),
	validate.FormatFromMetadata(),

	// Application names must be unique: the name is the key under which the
	// installed-source binding is recorded, so duplicates would fight over
	// the same state entry.
	validate.NoDuplicates("applications", "name"),

	// Every source listed must have its package block, and no listed source
	// may repeat. This cannot be expressed per field: it relates `source` to
	// the contents of `package`.
	spec.ValidatorFunc(validateSourceBlocks),
}

// validateSourceBlocks reports applications whose `source` list and `package`
// blocks disagree.
func validateSourceBlocks(in spec.ValidationInput) []spec.Violation {
	var doc config.Config
	if err := yaml.Unmarshal(in.Raw, &doc); err != nil {
		// Malformed YAML is reported by the editor itself; nothing to add.
		return nil
	}

	var out []spec.Violation
	for i, app := range doc.Applications {
		path := fmt.Sprintf("applications[%d]", i)

		seen := make(map[config.Source]bool, len(app.Source))
		for _, src := range app.Source {
			src = src.Normalize()
			if !src.Valid() {
				out = append(out, spec.Violation{
					Path:    path + ".source",
					Message: fmt.Sprintf("%q is not a valid source (winget, chocolatey, scoop, github)", src),
				})
				continue
			}
			if seen[src] {
				out = append(out, spec.Violation{
					Path:    path + ".source",
					Message: fmt.Sprintf("%q is listed more than once", src),
				})
				continue
			}
			seen[src] = true

			if id, ok := app.Package.ID(src); !ok || id == "" {
				out = append(out, spec.Violation{
					Path:    fmt.Sprintf("%s.package.%s.id", path, src),
					Message: fmt.Sprintf("required: %q is listed in source", src),
				})
			}
		}

		if app.Source.Contains(config.SourceGitHub) && app.EffectiveAction() == config.ActionUninstall {
			out = append(out, spec.Violation{
				Path:    path + ".action",
				Message: "uninstall is not supported for the github source",
			})
		}
	}
	return out
}
