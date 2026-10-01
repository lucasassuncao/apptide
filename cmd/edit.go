package cmd

import (
	"fmt"
	"time"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/bezel/theme"
	"github.com/lucasassuncao/bezel/themebrowser"
	"github.com/lucasassuncao/yedit/editor"
	"github.com/spf13/cobra"
)

var (
	editOutput           string
	editTheme            string
	editListThemes       bool
	editNoSaveConfirm    bool
	editNoDeleteConfirm  bool
	editNoValidateOnSave bool
	editDump             bool
	editDumpPath         string
)

// editHiddenKeys are the top-level keys the editor does not offer as blocks.
//
// schema_version is written by apptide, not chosen by the user: the loader
// defaults a file without it to the current version, and a file that carries
// an older one is converted on read. Editing the number by hand cannot make a
// file older or newer than its contents, so the field is dropped from the
// schema the editor builds its UI and its per-field rules from.
var editHiddenKeys = []string{"schema_version"}

// editPassthroughKeys are kept byte-for-byte and exempt from unknown-key
// validation. Every hidden key belongs here too: hiding a key removes it from
// the schema, which would otherwise make the file's own value read as an
// unknown key and block the save.
//
// import: is on the list for the other reason — the loader resolves it before
// the document is decoded, so it is not part of the schema to begin with.
var editPassthroughKeys = []string{"import", "schema_version"}

var editCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit the configuration file in an interactive TUI",
	Long: `Open the apptide configuration file in an interactive two-panel TUI editor.

The left panel lists the top-level blocks; Enter opens the block editor, where
fields are edited with type-appropriate controls and a hint panel explains what
each one does. Tab changes pane, Ctrl+S saves, Ctrl+U undoes, Ctrl+Y redoes,
Esc goes back and q quits. Press ? in the editor for every key.

Pressing p opens a picker: on the root list it offers whole-file templates —
the same ones apptide init writes — and inside a block editor it offers ready
made blocks (a winget application, a github binary, a source fallback list),
which Enter drops in place of the block and a appends to it.

Saving validates the whole config first, against the rules declared with each
field and the cross-field checks.

The top-level import: and schema_version: keys are preserved untouched and are
not offered as editable blocks: the first is resolved by the loader, and the
second is apptide's to write.`,
	Example: `  # Edit the resolved config file
  apptide edit

  # Edit with a different theme
  apptide edit --theme grape

  # Browse the available themes
  apptide edit --list-themes

  # Load one file but save to another (e.g. to bootstrap a new config)
  apptide edit --out work-packages.yaml

  # Record a session trace to attach to a bug report
  apptide edit --dump`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if editListThemes {
			return themebrowser.BrowseInTerminal()
		}

		selectedTheme, err := theme.Lookup(editTheme)
		if err != nil {
			return fmt.Errorf("%w: run 'apptide edit --list-themes' to see available themes", err)
		}

		loadPath, savePath := resolveEditPaths(configPath, editOutput)

		hints, err := config.NewMetadata()
		if err != nil {
			return fmt.Errorf("building hint source: %w", err)
		}

		res, err := editor.Run(editor.Config{
			Path:     loadPath,
			SavePath: savePath,
			Schema:   &config.Config{},
			Title:    "apptide",
			Metadata: hints,

			// p opens a picker: whole-file templates on the root list, ready
			// made blocks inside a block editor.
			DocPresets:   AppTideDocPresets,
			BlockPresets: AppTideBlockPresets,

			EnableHints: true,
			Theme:       selectedTheme,
			Validators:  AppTideValidators,

			Hidden:          editHiddenKeys,
			PassthroughKeys: editPassthroughKeys,

			NoSaveConfirm:    editNoSaveConfirm,
			NoDeleteConfirm:  editNoDeleteConfirm,
			NoValidateOnSave: editNoValidateOnSave,

			// Ease the hint panel open and closed instead of snapping.
			AnimationDuration: 600 * time.Millisecond,

			Trace: editor.Trace{
				Dump:     editDump || editDumpPath != "",
				DumpPath: editDumpPath,
			},
		})
		if err != nil {
			return err
		}

		if res.Saved {
			savedTo := loadPath
			if savePath != "" {
				savedTo = savePath
			}
			fmt.Println("configuration saved to", savedTo)
		}
		if res.DumpPath != "" {
			fmt.Println("session trace written to", res.DumpPath)
		}
		return nil
	},
}

// resolveEditPaths decides which file the editor loads and where it saves.
//
// --config wins. With only --out, that file is both loaded and saved, so
// `apptide edit --out new.yaml` bootstraps a file instead of failing on a missing
// one. Otherwise it edits the same file every other command reads, so editing
// and installing can never target different configs.
func resolveEditPaths(configFlag, output string) (loadPath, savePath string) {
	if configFlag != "" {
		return configFlag, output
	}
	if output != "" {
		return output, ""
	}
	// The same search order every other command uses, so edit and install
	// never end up on different files.
	return DefaultConfigPath(), ""
}

func init() {
	rootCmd.AddCommand(editCmd)
	editCmd.Flags().StringVar(&editOutput, "out", "", "save to this file instead of the loaded config")
	editCmd.Flags().StringVar(&editTheme, "theme", "plain", "theme name (run --list-themes to see options)")
	editCmd.Flags().BoolVar(&editListThemes, "list-themes", false, "browse available themes in an interactive terminal UI")
	editCmd.Flags().BoolVar(&editNoSaveConfirm, "no-save-confirm", false, "skip the 'Save changes?' confirmation dialog")
	editCmd.Flags().BoolVar(&editNoDeleteConfirm, "no-delete-confirm", false, "skip the 'Remove block?' confirmation dialog")
	editCmd.Flags().BoolVar(&editNoValidateOnSave, "no-validate-on-save", false, "allow saving even when validators report errors")
	editCmd.Flags().BoolVar(&editDump, "dump", false, "record every editor action to a JSONL trace file for bug reports")
	editCmd.Flags().StringVar(&editDumpPath, "dump-path", "", "write the session trace to this file instead of a temp file (implies --dump)")
}
