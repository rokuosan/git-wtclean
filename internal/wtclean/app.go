package wtclean

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type Options struct {
	DeleteMode string
	Prune      bool
	All        bool
	Verbose    bool
	Help       bool
	Version    bool
}

var Version = "dev"

type Runner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

type App struct {
	runner Runner
	stdout io.Writer
	stderr io.Writer
}

func NewApp(runner Runner, stdout, stderr io.Writer) *App {
	return &App{
		runner: runner,
		stdout: stdout,
		stderr: stderr,
	}
}

func (a *App) Run(ctx context.Context, args []string) int {
	opts, err := ParseArgs(args)
	if err != nil {
		writef(a.stderr, "git wtclean: %v\n", err)
		Usage(a.stderr)
		return 2
	}
	if opts.Help {
		Usage(a.stdout)
		return 0
	}
	if opts.Version {
		writef(a.stdout, "git-wtclean %s\n", Version)
		return 0
	}

	summary, err := a.run(ctx, opts)
	if err != nil {
		writef(a.stderr, "git wtclean: %v\n", err)
		return 127
	}

	a.printSummary(opts, summary)
	if summary.Failed > 0 {
		return 1
	}
	return 0
}

func (a *App) run(ctx context.Context, opts Options) (Summary, error) {
	var summary Summary

	repos, err := a.repositories(ctx, opts)
	if err != nil {
		return summary, err
	}

	for _, repo := range repos {
		if repo == "" {
			continue
		}
		a.processRepo(ctx, opts, repo, &summary)
	}

	return summary, nil
}

func (a *App) repositories(ctx context.Context, opts Options) ([]string, error) {
	if opts.All {
		return a.listRepositories(ctx)
	}

	repo, err := a.currentRepo(ctx)
	if err != nil {
		return nil, err
	}
	return []string{repo}, nil
}

func (a *App) listRepositories(ctx context.Context) ([]string, error) {
	out, err := a.runner.Output(ctx, "ghq", "list", "-p")
	if err != nil {
		return nil, fmt.Errorf("ghq list -p failed: %w", err)
	}
	return splitLines(out), nil
}

// currentRepo resolves the repository containing the current directory and
// returns the path of its primary worktree. Linked worktrees share the same
// worktree list, so resolving to the primary keeps the rest of the pipeline
// consistent regardless of which worktree the command was invoked from.
func (a *App) currentRepo(ctx context.Context) (string, error) {
	out, err := a.runner.Output(ctx, "git", "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", fmt.Errorf("not inside a git repository (use --all to target all ghq repositories): %w", err)
	}

	worktrees := ParseWorktreeList(out)
	if len(worktrees) == 0 || worktrees[0].Path == "" {
		return "", errors.New("could not determine the current repository (use --all to target all ghq repositories)")
	}
	return worktrees[0].Path, nil
}

func (a *App) processRepo(ctx context.Context, opts Options, repo string, summary *Summary) {
	if !a.isGitRepo(ctx, repo) {
		return
	}

	// In pure prune mode (--prune without a delete option), only clean stale
	// worktree metadata and skip listing active worktrees.
	if opts.DeleteMode != "" || !opts.Prune {
		paths, err := a.listLinkedWorktreePaths(ctx, repo)
		if err != nil {
			summary.Failed++
			writef(a.stderr, "git wtclean: failed to list worktrees: %s: %v\n", repo, err)
			return
		}

		for _, path := range paths {
			summary.Found++
			if opts.DeleteMode == "" {
				writef(a.stdout, "Would remove: %s\n", path)
				continue
			}
			a.removeWorktree(ctx, opts, repo, path, summary)
		}
	}

	if opts.Prune {
		a.pruneRepo(ctx, opts, repo, summary)
	}
}

func (a *App) isGitRepo(ctx context.Context, repo string) bool {
	_, err := a.runner.Output(ctx, "git", "-C", repo, "rev-parse", "--git-dir")
	return err == nil
}

func (a *App) listLinkedWorktreePaths(ctx context.Context, repo string) ([]string, error) {
	out, err := a.runner.Output(ctx, "git", "-C", repo, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}

	worktrees := ParseWorktreeList(out)
	paths := make([]string, 0, len(worktrees))
	for i, wt := range worktrees {
		if i == 0 || wt.Bare || wt.Path == "" {
			continue
		}
		paths = append(paths, wt.Path)
	}
	return paths, nil
}

func (a *App) removeWorktree(ctx context.Context, opts Options, repo, path string, summary *Summary) {
	writef(a.stdout, "Removing (%s): %s\n", opts.DeleteMode, path)

	args := []string{"-C", repo, "worktree", "remove"}
	if opts.DeleteMode == "-D" {
		args = append(args, "--force")
	}
	args = append(args, path)

	out, err := a.runner.Output(ctx, "git", args...)
	if len(out) > 0 {
		writeString(a.stdout, string(out))
	}
	if err != nil {
		summary.Failed++
		writef(a.stderr, "git wtclean: failed to remove: %s\n", path)
		return
	}
	summary.Removed++
}

func (a *App) pruneRepo(ctx context.Context, opts Options, repo string, summary *Summary) {
	if opts.Verbose {
		writef(a.stdout, "Pruning: %s\n", repo)
	}

	out, err := a.runner.Output(ctx, "git", "-C", repo, "worktree", "prune")
	if len(out) > 0 {
		writeString(a.stdout, string(out))
	}
	if err != nil {
		summary.Failed++
		writef(a.stderr, "git wtclean: failed to prune: %s\n", repo)
		return
	}
	summary.PruneChecked++
}

func (a *App) printSummary(opts Options, summary Summary) {
	if opts.DeleteMode == "" {
		if opts.Prune {
			writef(a.stdout, "Done. prune_checked=%d failed=%d\n", summary.PruneChecked, summary.Failed)
			return
		}
		writef(a.stdout, "Dry run. found=%d. Run %s to remove, or %s to force remove.\n", summary.Found, "'git wtclean -d'", "'git wtclean -D'")
		return
	}
	writef(a.stdout, "Done. found=%d removed=%d prune_checked=%d failed=%d\n", summary.Found, summary.Removed, summary.PruneChecked, summary.Failed)
}

func ParseArgs(args []string) (Options, error) {
	var opts Options

	for _, arg := range args {
		switch arg {
		case "-d", "-D":
			if opts.DeleteMode != "" {
				return opts, errors.New("specify only one delete option: -d or -D")
			}
			opts.DeleteMode = arg
		case "--prune":
			opts.Prune = true
		case "--all":
			opts.All = true
		case "-v", "--verbose":
			opts.Verbose = true
		case "--version":
			opts.Version = true
		case "-h", "--help":
			opts.Help = true
		default:
			return opts, fmt.Errorf("unknown option: %s", arg)
		}
	}

	return opts, nil
}

func Usage(w io.Writer) {
	writeString(w, `Usage:
  git wtclean           Show linked worktrees in the current repository that would be removed
  git wtclean -d        Remove them with `+"`git worktree remove`"+`
  git wtclean -D        Force remove them with `+"`git worktree remove --force`"+`
  git wtclean --prune   Prune stale worktree metadata with `+"`git worktree prune`"+`
  git wtclean --all     Target every repository listed by `+"`ghq list -p`"+` instead of just the current one

Options:
  -d                    Remove worktrees with `+"`git worktree remove`"+`
  -D                    Force remove worktrees with `+"`git worktree remove --force`"+`
  --prune               Run `+"`git worktree prune`"+`
  --all                 Target every ghq repository instead of the current one
  -v, --verbose         Print each repository while pruning
  --version             Show version
  -h                    Show this help

Note:
  When invoked as a Git subcommand, use `+"`git wtclean -h`"+`.
  `+"`git wtclean --help`"+` is handled by Git itself and opens the git-wtclean(1) man page.
`)
}

func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func writeString(w io.Writer, s string) {
	_, _ = io.WriteString(w, s)
}

type Summary struct {
	Found        int
	Removed      int
	Failed       int
	PruneChecked int
}

type Worktree struct {
	Path string
	Bare bool
}

func ParseWorktreeList(data []byte) []Worktree {
	fields := bytes.Split(data, []byte{0})
	worktrees := make([]Worktree, 0)
	var current *Worktree

	for _, field := range fields {
		if len(field) == 0 {
			if current != nil {
				worktrees = append(worktrees, *current)
				current = nil
			}
			continue
		}

		line := string(field)
		if strings.HasPrefix(line, "worktree ") {
			if current != nil {
				worktrees = append(worktrees, *current)
			}
			current = &Worktree{Path: strings.TrimPrefix(line, "worktree ")}
			continue
		}
		if current == nil {
			continue
		}
		if line == "bare" {
			current.Bare = true
		}
	}

	if current != nil {
		worktrees = append(worktrees, *current)
	}
	return worktrees
}

func splitLines(data []byte) []string {
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}
