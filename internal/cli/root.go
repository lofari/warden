package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/winler/warden/internal/runtime"
	_ "github.com/winler/warden/internal/runtime/docker"
	_ "github.com/winler/warden/internal/runtime/firecracker"
	_ "github.com/winler/warden/internal/runtime/sandbox"
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
		authBroker   bool
		ephemeral    bool
	)

	run := &cobra.Command{
		Use:   "run [flags] -- <command...>",
		Short: "Run a command in a sandboxed container",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var net *bool
			if cmd.Flags().Changed("network") && cmd.Flags().Changed("no-network") {
				return fmt.Errorf("--network and --no-network are mutually exclusive")
			}
			if cmd.Flags().Changed("network") {
				v := true
				net = &v
			}
			if cmd.Flags().Changed("no-network") {
				v := false
				net = &v
			}

			opts := resolveOptions{
				network: net,
				profile: profile,
			}
			if cmd.Flags().Changed("image") {
				opts.image = image
			}
			if cmd.Flags().Changed("memory") {
				opts.memory = memory
			}
			if cmd.Flags().Changed("cpus") {
				opts.cpus = cpus
			}
			if cmd.Flags().Changed("timeout") {
				opts.timeout = timeout
			}
			if cmd.Flags().Changed("workdir") {
				opts.workdir = workdir
			}
			if cmd.Flags().Changed("tools") {
				opts.tools = tools
			}
			if cmd.Flags().Changed("display") {
				opts.display = display
			}
			if cmd.Flags().Changed("resolution") {
				opts.resolution = resolution
			}
			if len(proxyFlags) > 0 {
				opts.proxy = proxyFlags
			}
			if cmd.Flags().Changed("auth-broker") && authBroker {
				opts.authBroker = true
			}
			if cmd.Flags().Changed("ephemeral") {
				opts.ephemeral = ephemeral
			}
			if len(envFlags) > 0 {
				opts.env = envFlags
			}
			if len(mountFlags) > 0 {
				opts.mounts = mountFlags
			}
			if cmd.Flags().Changed("runtime") {
				opts.runtime = runtimeFlag
			}

			if dryRun {
				cfg, err := resolveConfig(opts)
				if err != nil {
					return err
				}
				rt, resolvedName, err := runtime.ResolveRuntime(cfg.Runtime)
				if err != nil {
					return err
				}
				if cfg.Display && resolvedName != "firecracker" {
					return fmt.Errorf("--display is only supported with firecracker runtime")
				}
				return rt.DryRun(cfg, args)
			}

			return resolveAndRun(opts, args)
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
	run.Flags().StringVar(&runtimeFlag, "runtime", "", "Runtime backend (docker, sandbox, or firecracker)")
	run.Flags().BoolVar(&display, "display", false, "Enable virtual display (Firecracker only)")
	run.Flags().StringVar(&resolution, "resolution", "1280x1024x24", "Display resolution (e.g. 1920x1080x24)")
	run.Flags().StringArrayVar(&proxyFlags, "proxy", nil, "Proxy a command to the host (can be repeated)")
	run.Flags().BoolVar(&authBroker, "auth-broker", false, "Enable auth broker for Claude API proxying")
	run.Flags().BoolVar(&ephemeral, "ephemeral", false, "Remove sandbox after execution (sandbox runtime only)")

	root.AddCommand(run)
	root.AddCommand(newInitCommand())
	root.AddCommand(newImagesCommand())
	root.AddCommand(newSetupCommand())
	root.AddCommand(newPsCommand())
	root.AddCommand(newStopCommand())
	root.AddCommand(newRmCommand())
	root.AddCommand(newShellCommand())
	root.AddCommand(newExecCommand())
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
