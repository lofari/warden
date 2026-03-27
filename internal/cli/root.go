package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime"
	_ "github.com/winler/warden/internal/runtime/docker"
	_ "github.com/winler/warden/internal/runtime/firecracker"
)

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "warden",
		Short: "Secure sandbox for AI coding agents",
	}

	var (
		mountFlags   []string
		envFlags     []string
		network      bool
		noNetwork    bool
		timeout      string
		memory       string
		cpus         int
		tools        string
		image        string
		profile      string
		workdir      string
		dryRun       bool
		runtimeFlag  string
		display      bool
		resolution   string
		proxyFlags   []string
	)

	run := &cobra.Command{
		Use:   "run [flags] -- <command...>",
		Short: "Run a command in a sandboxed container",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 1. Load .warden.yaml if it exists
			cfg := config.DefaultConfig()
			wardenPath := findWardenYAML()
			baseDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("warden: cannot determine working directory: %w", err)
			}

			if wardenPath != "" {
				data, err := os.ReadFile(wardenPath)
				if err != nil {
					return fmt.Errorf("reading %s: %w", wardenPath, err)
				}
				file, err := config.ParseWardenYAML(data)
				if err != nil {
					return err
				}
				baseDir = filepath.Dir(wardenPath)
				resolved, err := config.ResolveProfile(file, profile)
				if err != nil {
					return err
				}
				cfg = resolved
			}

			// 2. CLI flag overrides
			if cmd.Flags().Changed("image") {
				cfg.Image = image
			}
			if cmd.Flags().Changed("memory") {
				cfg.Memory = memory
			}
			if cmd.Flags().Changed("cpus") {
				cfg.CPUs = cpus
			}
			if cmd.Flags().Changed("timeout") {
				cfg.Timeout = timeout
			}
			if cmd.Flags().Changed("workdir") {
				cfg.Workdir = workdir
			}
			if cmd.Flags().Changed("tools") {
				cfg.Tools = strings.Split(tools, ",")
			}
			if cmd.Flags().Changed("network") {
				cfg.Network = true
			}
			if cmd.Flags().Changed("no-network") {
				cfg.Network = false
			}

			if cmd.Flags().Changed("network") && cmd.Flags().Changed("no-network") {
				return fmt.Errorf("--network and --no-network are mutually exclusive")
			}

			if cmd.Flags().Changed("display") {
				cfg.Display = display
			}
			if cmd.Flags().Changed("resolution") {
				cfg.Resolution = resolution
			}

			// Proxy overrides from CLI
			if len(proxyFlags) > 0 {
				cfg.Proxy = proxyFlags
			}

			// 3. Env overrides from CLI
			if len(envFlags) > 0 {
				cfg.Env = envFlags
			}

			// 4. Mount overrides from CLI
			if len(mountFlags) > 0 {
				cfg.Mounts = nil
				for _, m := range mountFlags {
					parts := strings.SplitN(m, ":", 2)
					mode := "ro"
					if len(parts) == 2 {
						mode = parts[1]
					}
					cfg.Mounts = append(cfg.Mounts, config.Mount{Path: parts[0], Mode: mode})
				}
			}

			// 5. Default: mount cwd as rw if no mounts specified
			if len(cfg.Mounts) == 0 {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("warden: cannot determine working directory: %w", err)
				}
				cfg.Mounts = []config.Mount{{Path: cwd, Mode: "rw"}}
			}

			// 6. Resolve mount paths
			resolvedMounts, err := runtime.ResolveMounts(cfg.Mounts, baseDir)
			if err != nil {
				return err
			}
			cfg.Mounts = resolvedMounts

			// Validate config before dispatching to runtime
			if err := cfg.Validate(); err != nil {
				return err
			}

			// 7. Default workdir to first rw mount
			if cfg.Workdir == "" {
				for _, m := range cfg.Mounts {
					if m.Mode == "rw" {
						cfg.Workdir = m.Path
						break
					}
				}
			}

			// 8. Select runtime
			rtName := cfg.Runtime
			if cmd.Flags().Changed("runtime") {
				rtName = runtimeFlag
			}
			rt, err := runtime.NewRuntime(rtName)
			if err != nil {
				return err
			}

			if cfg.Display && rtName != "firecracker" {
				return fmt.Errorf("--display is only supported with firecracker runtime")
			}

			// 9. Dry-run does NOT require Preflight
			if dryRun {
				return rt.DryRun(cfg, args)
			}

			// 10. Preflight
			if err := rt.Preflight(); err != nil {
				return err
			}

			if _, err := rt.EnsureImage(cfg); err != nil {
				return err
			}

			exitCode, err := rt.Run(cfg, args)
			if err != nil {
				return err
			}
			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return nil
		},
	}

	run.Flags().StringArrayVar(&mountFlags, "mount", nil, "Mount host path (path:mode, mode is ro or rw)")
	run.Flags().StringArrayVar(&envFlags, "env", nil, "Environment variable (KEY=VALUE or KEY to pass through from host)")
	run.Flags().BoolVar(&network, "network", false, "Enable networking")
	run.Flags().BoolVar(&noNetwork, "no-network", false, "Disable networking")
	run.Flags().StringVar(&timeout, "timeout", "", "Max execution time (e.g. 30m, 2h)")
	run.Flags().StringVar(&memory, "memory", "", "Memory limit (e.g. 4g)")
	run.Flags().IntVar(&cpus, "cpus", 0, "CPU limit")
	run.Flags().StringVar(&tools, "tools", "", "Dev tools to install (comma-separated: node,python,go,rust,java)")
	run.Flags().StringVar(&image, "image", "", "Base image (default: ubuntu:24.04)")
	run.Flags().StringVar(&profile, "profile", "", "Profile from .warden.yaml")
	run.Flags().StringVar(&workdir, "workdir", "", "Working directory inside container")
	run.Flags().BoolVar(&dryRun, "dry-run", false, "Print docker command without executing")
	run.Flags().StringVar(&runtimeFlag, "runtime", "", "Runtime backend (docker or firecracker)")
	run.Flags().BoolVar(&display, "display", false, "Enable virtual display (Firecracker only)")
	run.Flags().StringVar(&resolution, "resolution", "1280x1024x24", "Display resolution (e.g. 1920x1080x24)")
	run.Flags().StringArrayVar(&proxyFlags, "proxy", nil, "Proxy a command to the host (can be repeated)")

	root.AddCommand(run)
	root.AddCommand(newInitCommand())
	root.AddCommand(newImagesCommand())
	root.AddCommand(newSetupCommand())
	root.AddCommand(newPsCommand())
	return root
}

// findWardenYAML walks up from cwd to find .warden.yaml.
func findWardenYAML() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		path := filepath.Join(dir, ".warden.yaml")
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
