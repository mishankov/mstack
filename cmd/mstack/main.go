package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
)

const upstream = "https://github.com/mishankov/mstack.git"

const usage = `Manage mstack skills in the current Git project.

Usage:
  mstack list
  mstack install <skill>...
  mstack install --all
  mstack update [<skill>...]
  mstack update --check [<skill>...]
  mstack remove <skill>...
  mstack --version

Skills are installed in .agents/skills/ at the Git root.
--all selects the bundled skills and their dependencies.
`

type request struct {
	command string
	names   []string
	all     bool
	check   bool
}

func parse(args []string) (request, error) {
	if len(args) == 0 {
		return request{command: "help"}, nil
	}
	r := request{command: args[0]}
	switch r.command {
	case "--help", "-h", "help":
		return request{command: "help"}, nil
	case "--version", "list", "install", "update", "remove":
	default:
		return request{}, fmt.Errorf("unknown command %q (see mstack --help)", r.command)
	}
	for _, arg := range args[1:] {
		switch {
		case arg == "--help" || arg == "-h":
			return request{command: "help"}, nil
		case r.command == "install" && arg == "--all":
			r.all = true
		case r.command == "update" && arg == "--check":
			r.check = true
		case (r.command == "install" || r.command == "update" || r.command == "remove") && validName(arg):
			r.names = append(r.names, arg)
		default:
			return request{}, fmt.Errorf("unexpected argument %q for %s (see mstack --help)", arg, r.command)
		}
	}
	if r.command == "install" {
		if r.all && len(r.names) > 0 {
			return request{}, fmt.Errorf("pass skill names or --all to install")
		}
		if !r.all && len(r.names) == 0 {
			return request{}, fmt.Errorf("install requires skill names or --all (see mstack --help)")
		}
	}
	if r.command == "remove" && len(r.names) == 0 {
		return request{}, fmt.Errorf("remove requires skill names (see mstack --help)")
	}
	return r, nil
}

func run(ctx context.Context, cwd, source string, args []string, out io.Writer) error {
	r, err := parse(args)
	if err != nil {
		return err
	}
	switch r.command {
	case "help":
		fmt.Fprint(out, usage)
		return nil
	case "--version":
		version := "(devel)"
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
			version = info.Main.Version
		}
		fmt.Fprintln(out, "mstack", version)
		return nil
	}
	root, err := git(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("find the current project's Git root: %w", err)
	}
	p := openProject(strings.TrimSuffix(string(root), "\n"))
	execute := func() error { return p.execute(ctx, source, r, out) }
	if r.command == "list" || r.check {
		if err := p.checkWriter(); err != nil {
			return err
		}
		return execute()
	}
	if r.command == "update" || r.command == "remove" {
		present, err := directoryExists(p.agents)
		if err != nil {
			return err
		}
		if !present {
			if r.command == "remove" {
				return fmt.Errorf("skill %q is not managed by mstack", r.names[0])
			}
			if _, err := updateTargets(r.names, lockFile{}); err != nil {
				return err
			}
			fmt.Fprintln(out, "No mstack-managed skills installed.")
			return nil
		}
	}
	return p.withWriter(execute)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, "", upstream, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mstack:", err)
		os.Exit(1)
	}
}
