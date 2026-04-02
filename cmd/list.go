package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/spf13/cobra"
)

var listCategories bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List packages defined in the config file",
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
			type jsonPkg struct {
				Category    string `json:"category"`
				Name        string `json:"name"`
				Source      string `json:"source"`
				Action      string `json:"action"`
				Version     string `json:"version,omitempty"`
				Description string `json:"description,omitempty"`
			}
			var all []jsonPkg
			for _, cat := range categories {
				pkgs := make([]config.Package, len(cfg[cat]))
				copy(pkgs, cfg[cat])
				sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
				for _, pkg := range pkgs {
					action := strings.ToLower(pkg.Action)
					if action == "" {
						action = "install"
					}
					all = append(all, jsonPkg{
						Category:    cat,
						Name:        pkg.Name,
						Source:      pkg.Source,
						Action:      action,
						Version:     pkg.Version,
						Description: pkg.Description,
					})
				}
			}
			output.PrintJSON(all)
			return nil
		}

		if listCategories {
			fmt.Println("Available categories:")
			for _, c := range categories {
				fmt.Printf("  %-24s %d packages\n", c, len(cfg[c]))
			}
			return nil
		}

		// Compute the widest name across all packages so columns align dynamically.
		wName := 20
		for _, cat := range categories {
			for _, pkg := range cfg[cat] {
				if l := len(pkg.Name); l > wName {
					wName = l
				}
			}
		}
		wName += 2 // breathing room

		for _, cat := range categories {
			fmt.Printf("\n\033[1m\033[33m[%s]\033[0m\n", cat)

			pkgs := make([]config.Package, len(cfg[cat]))
			copy(pkgs, cfg[cat])
			sort.Slice(pkgs, func(i, j int) bool {
				return pkgs[i].Name < pkgs[j].Name
			})

			for _, pkg := range pkgs {
				action := strings.ToLower(pkg.Action)
				if action == "" {
					action = "install"
				}

				actionColor := "\033[32m" // green = install
				switch action {
				case "skip":
					actionColor = "\033[90m"
				case "uninstall":
					actionColor = "\033[31m"
				}

				desc := ""
				if pkg.Description != "" {
					desc = fmt.Sprintf("  \033[90m%s\033[0m", pkg.Description)
				}

				fmt.Printf("  %-*s \033[36m%-12s\033[0m %s%-10s\033[0m%s\n",
					wName, pkg.Name, pkg.Source, actionColor, action, desc)
			}
		}

		fmt.Println()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().BoolVar(&listCategories, "categories", false, "list only category names")
}
