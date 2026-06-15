# git-wtclean

`git-wtclean` is a Git subcommand for cleaning linked worktrees under repositories managed by [`ghq`](https://github.com/x-motemen/ghq).

It uses Git's native worktree commands:

- `git worktree list --porcelain -z`
- `git worktree remove`
- `git worktree remove --force`
- `git worktree prune`

It does not delete branches.

This tool is inspired by [`k1LoW/git-wt`](https://github.com/k1LoW/git-wt), but does not depend on it.

## Install

```sh
go install github.com/rokuosan/git-wtclean/cmd/git-wtclean@latest
```

Make sure the installed `git-wtclean` binary is in your `PATH`. Git will then expose it as:

```sh
git wtclean
```

## Usage

Dry-run by default:

```sh
git wtclean
```

Remove linked worktrees:

```sh
git wtclean -d
```

Force remove linked worktrees:

```sh
git wtclean -D
```

Prune stale worktree metadata for each ghq repository:

```sh
git wtclean --prune
```

Print each repository while pruning:

```sh
git wtclean --prune --verbose
git wtclean --prune -v
```

## What It Removes

`git wtclean` targets linked worktrees discovered from:

```sh
ghq list -p
git -C <repo> worktree list --porcelain -z
```

The first worktree entry is treated as the primary worktree and is skipped. Bare worktree entries are also skipped.

With `-d`, each target is removed with:

```sh
git -C <repo> worktree remove <path>
```

With `-D`, each target is removed with:

```sh
git -C <repo> worktree remove --force <path>
```

`--prune` runs:

```sh
git -C <repo> worktree prune
```

This only cleans stale Git worktree metadata, such as records left after a worktree directory was removed manually. It does not remove branches.

## Help

Use `-h` for the built-in help:

```sh
git wtclean -h
```

When invoked as a Git subcommand, `git wtclean --help` is handled by Git itself and opens the `git-wtclean(1)` man page if installed.

## Development

This repository uses [`mise`](https://mise.jdx.dev/) for tool versions and tasks.

```sh
mise run format
mise run lint
```

You can also run the Go checks directly:

```sh
go test ./...
go vet ./...
golangci-lint run
```
