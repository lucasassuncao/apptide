package cmd

import (
	"fmt"
	"math"
	"sort"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/lucasassuncao/bezel/table"
	"github.com/spf13/cobra"
)

var listCategories bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List applications defined in the config file",
	Example: `  apptide list
  apptide list --categories
  apptide list --config other.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadWithImports(configPath)
		if err != nil {
			return err
		}

		categories := cfg.Categories()

		if output.IsJSON() {
			return listJSON(cfg, categories)
		}
		if listCategories {
			rows := make([][]string, len(categories))
			for i, c := range categories {
				rows[i] = []string{c, fmt.Sprintf("%d application(s)", len(cfg.InCategory(c)))}
			}
			widths := table.Fit(make([]table.Column, 2), rows, math.MaxInt)
			fmt.Println("Available categories:")
			for _, r := range rows {
				fmt.Println("  " + table.Row(widths, r))
			}
			return nil
		}
		return listTable(cfg, categories)
	},
}

// jsonApp is the shape emitted by `list --output json`. Sources is a list
// because an application may accept several managers in preference order.
type jsonApp struct {
	Category    string   `json:"category"`
	Name        string   `json:"name"`
	Sources     []string `json:"sources"`
	Action      string   `json:"action"`
	Version     string   `json:"version,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Description string   `json:"description,omitempty"`
}

func listJSON(cfg *config.Config, categories []string) error {
	var all []jsonApp
	for _, cat := range categories {
		apps := sortedByName(cfg.InCategory(cat))
		for _, app := range apps {
			sources := make([]string, len(app.Source))
			for i, s := range app.Source {
				sources[i] = string(s)
			}
			all = append(all, jsonApp{
				Category:    cat,
				Name:        app.Name,
				Sources:     sources,
				Action:      string(app.EffectiveAction()),
				Version:     app.Version,
				Tags:        app.Tags,
				Description: app.Description,
			})
		}
	}
	output.PrintJSON(all)
	return nil
}

func listTable(cfg *config.Config, categories []string) error {
	// Sized once over every category, so the columns line up across groups.
	rows := make(map[string][][]string, len(categories))
	var all [][]string
	for _, cat := range categories {
		for _, app := range sortedByName(cfg.InCategory(cat)) {
			action := app.EffectiveAction()
			actionStyle := th.Success
			switch action {
			case config.ActionSkip:
				actionStyle = th.Dim
			case config.ActionUninstall:
				actionStyle = th.Danger
			}
			r := []string{
				app.Name,
				th.Info.Render(app.Source.String()),
				actionStyle.Render(string(action)),
				th.Dim.Render(app.Description),
			}
			rows[cat] = append(rows[cat], r)
			all = append(all, r)
		}
	}
	widths := table.Fit(make([]table.Column, 4), all, math.MaxInt)

	for _, cat := range categories {
		fmt.Println("\n" + th.Warning.Bold(true).Render("["+cat+"]"))
		for _, r := range rows[cat] {
			fmt.Println("  " + table.Row(widths, r))
		}
	}

	fmt.Println()
	return nil
}

func sortedByName(apps []config.Application) []config.Application {
	out := make([]config.Application, len(apps))
	copy(out, apps)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().BoolVar(&listCategories, "categories", false, "list only category names")
}
