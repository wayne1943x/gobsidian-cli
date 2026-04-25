package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"gobsidian-cli/internal/app"
	"gobsidian-cli/internal/config"
	"gobsidian-cli/internal/plugin"
	"gobsidian-cli/internal/vaultops"
)

func Run(args []string, stdout, stderr io.Writer, registry *plugin.Registry) error {
	var configPath string
	var vaultName string
	var syncOpts plugin.SyncOptions
	root := &cobra.Command{
		Use:           "gobsidian",
		Short:         "Agent-friendly Obsidian CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().StringVarP(&configPath, "config", "c", "", "config file")

	load := func() (config.Config, error) {
		path, err := config.ResolvePath(configPath)
		if err != nil {
			return config.Config{}, err
		}
		cfg, err := config.Load(path)
		if err != nil {
			return config.Config{}, err
		}
		if vaultName != "" {
			return cfg.FilterTargets(vaultName)
		}
		return cfg, nil
	}
	loadOneVault := func() (config.Target, error) {
		cfg, err := load()
		if err != nil {
			return config.Target{}, err
		}
		if len(cfg.Targets) == 1 {
			return cfg.Targets[0], nil
		}
		names := make([]string, 0, len(cfg.Targets))
		for _, target := range cfg.Targets {
			names = append(names, target.Name)
		}
		return config.Target{}, fmt.Errorf("multiple vaults configured; pass --vault (%s)", strings.Join(names, ", "))
	}
	runner := app.New(registry)
	syncCmd := &cobra.Command{
		Use:   "sync",
		Short: "Synchronize Obsidian vaults once",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if syncOpts.ForceRemote && syncOpts.ForceLocal {
				return fmt.Errorf("--force-remote and --force-local cannot be used together")
			}
			cfg, err := load()
			if err != nil {
				return err
			}
			resp := runner.Sync(context.Background(), cfg, syncOpts)
			if err := writeJSON(stdout, resp); err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf("sync failed")
			}
			return nil
		},
	}
	syncCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	syncCmd.Flags().BoolVar(&syncOpts.ForceRemote, "force-remote", false, "prefer remote data when resolving sync state")
	syncCmd.Flags().BoolVar(&syncOpts.ForceLocal, "force-local", false, "prefer local data when resolving sync state")
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Print Obsidian vault status",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := load()
			if err != nil {
				return err
			}
			resp := runner.Status(context.Background(), cfg)
			if err := writeJSON(stdout, resp); err != nil {
				return err
			}
			if !resp.OK {
				return fmt.Errorf("status failed")
			}
			return nil
		},
	}
	statusCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")

	var searchOpts vaultops.SearchOptions
	searchCmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search local Obsidian notes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			target, err := loadOneVault()
			if err != nil {
				return err
			}
			opts := searchOpts
			if len(args) == 1 {
				opts.Query = args[0]
			}
			resp, err := vaultops.Search(target.Vault.Path, target.Name, opts)
			if err != nil {
				return err
			}
			return writeJSON(stdout, resp)
		},
	}
	searchCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	searchCmd.Flags().StringVar(&searchOpts.TitleQuery, "title", "", "search note title or path")
	searchCmd.Flags().StringArrayVar(&searchOpts.Tags, "tag", nil, "required tag; repeat for AND filter")
	searchCmd.Flags().IntVar(&searchOpts.Limit, "limit", 20, "maximum results")
	searchCmd.Flags().IntVar(&searchOpts.Offset, "offset", 0, "result offset")
	searchCmd.Flags().BoolVar(&searchOpts.CaseSensitive, "case-sensitive", false, "case-sensitive search")
	searchCmd.Flags().BoolVar(&searchOpts.IncludeHidden, "include-hidden", false, "include hidden files")

	var readOpts vaultops.ReadOptions
	readCmd := &cobra.Command{
		Use:   "read <note>",
		Short: "Read a local Obsidian note",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			target, err := loadOneVault()
			if err != nil {
				return err
			}
			opts := readOpts
			opts.Ref = args[0]
			resp, err := vaultops.Read(target.Vault.Path, target.Name, opts)
			if err != nil {
				return err
			}
			if opts.JSON {
				return writeJSON(stdout, resp)
			}
			_, err = fmt.Fprint(stdout, resp.Content)
			return err
		},
	}
	readCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	readCmd.Flags().BoolVar(&readOpts.JSON, "json", false, "print structured JSON")
	readCmd.Flags().IntVar(&readOpts.Head, "head", 0, "print first N lines")
	readCmd.Flags().IntVar(&readOpts.Tail, "tail", 0, "print last N lines")
	readCmd.Flags().StringVar(&readOpts.Range, "range", "", "print line range start:end")
	readCmd.Flags().IntVar(&readOpts.MaxBytes, "max-bytes", 0, "print at most N bytes")

	var listOpts vaultops.ListOptions
	listCmd := &cobra.Command{
		Use:   "list [folder]",
		Short: "List local Obsidian vault files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			target, err := loadOneVault()
			if err != nil {
				return err
			}
			opts := listOpts
			if len(args) == 1 {
				opts.Folder = args[0]
			}
			resp, err := vaultops.List(target.Vault.Path, target.Name, opts)
			if err != nil {
				return err
			}
			return writeJSON(stdout, resp)
		},
	}
	listCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	listCmd.Flags().BoolVar(&listOpts.Recursive, "recursive", false, "list recursively")
	listCmd.Flags().IntVar(&listOpts.Depth, "depth", 0, "maximum relative depth")
	listCmd.Flags().StringVar(&listOpts.Type, "type", "all", "entry type: all, note, file, dir")
	listCmd.Flags().BoolVar(&listOpts.IncludeHidden, "include-hidden", false, "include hidden files")

	var moveOpts vaultops.MoveOptions
	moveOpts.UpdateLinks = true
	moveCmd := &cobra.Command{
		Use:   "move <from> <to>",
		Short: "Move a local Obsidian note",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := loadOneVault()
			if err != nil {
				return err
			}
			opts := moveOpts
			opts.From = args[0]
			opts.To = args[1]
			noUpdate, _ := cmd.Flags().GetBool("no-update-links")
			opts.UpdateLinks = !noUpdate
			resp, err := vaultops.Move(target.Vault.Path, target.Name, opts)
			if err != nil {
				return err
			}
			return writeJSON(stdout, resp)
		},
	}
	moveCmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
	moveCmd.Flags().BoolVar(&moveOpts.Overwrite, "overwrite", false, "overwrite destination")
	moveCmd.Flags().BoolVar(&moveOpts.DryRun, "dry-run", false, "show changes without writing")
	moveCmd.Flags().Bool("no-update-links", false, "do not update links")

	frontmatterCmd := &cobra.Command{
		Use:     "frontmatter",
		Aliases: []string{"fm"},
		Short:   "Read and edit note frontmatter",
	}
	fmGetCmd := &cobra.Command{
		Use:   "get <note> [key]",
		Short: "Read frontmatter",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			target, err := loadOneVault()
			if err != nil {
				return err
			}
			key := ""
			if len(args) == 2 {
				key = args[1]
			}
			resp, err := vaultops.FrontmatterGet(target.Vault.Path, target.Name, args[0], key)
			if err != nil {
				return err
			}
			return writeJSON(stdout, resp)
		},
	}
	fmSetCmd := &cobra.Command{
		Use:   "set <note> <key> <yaml-value>",
		Short: "Set frontmatter value",
		Args:  cobra.ExactArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			target, err := loadOneVault()
			if err != nil {
				return err
			}
			resp, err := vaultops.FrontmatterSet(target.Vault.Path, target.Name, args[0], args[1], args[2])
			if err != nil {
				return err
			}
			return writeJSON(stdout, resp)
		},
	}
	fmDeleteCmd := &cobra.Command{
		Use:   "delete <note> <key>",
		Short: "Delete frontmatter value",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			target, err := loadOneVault()
			if err != nil {
				return err
			}
			resp, err := vaultops.FrontmatterDelete(target.Vault.Path, target.Name, args[0], args[1])
			if err != nil {
				return err
			}
			return writeJSON(stdout, resp)
		},
	}
	for _, cmd := range []*cobra.Command{fmGetCmd, fmSetCmd, fmDeleteCmd} {
		cmd.Flags().StringVarP(&vaultName, "vault", "v", "", "vault name")
		frontmatterCmd.AddCommand(cmd)
	}
	root.AddCommand(syncCmd, statusCmd, searchCmd, readCmd, listCmd, moveCmd, frontmatterCmd)
	root.SetArgs(args)
	return root.Execute()
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
