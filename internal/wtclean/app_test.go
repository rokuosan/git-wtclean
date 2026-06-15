package wtclean

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	data := []byte(strings.Join([]string{
		"worktree /repo",
		"HEAD 1111111111111111111111111111111111111111",
		"branch refs/heads/main",
		"",
		"worktree /repo/.wt/feature",
		"HEAD 2222222222222222222222222222222222222222",
		"branch refs/heads/feature",
		"",
		"worktree /repo/.wt/bare",
		"bare",
		"",
	}, "\x00"))

	got := ParseWorktreeList(data)
	want := []Worktree{
		{Path: "/repo"},
		{Path: "/repo/.wt/feature"},
		{Path: "/repo/.wt/bare", Bare: true},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseWorktreeList() = %#v, want %#v", got, want)
	}
}

func TestRunDryRunListsLinkedWorktrees(t *testing.T) {
	app, runner, stdout, stderr := newTestApp()

	code := app.Run(context.Background(), nil)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}

	want := strings.Join([]string{
		"Would remove: /tmp/repo1/.wt/feature-a",
		"Would remove: /tmp/repo2/.wt/feature-b",
		"Dry run. found=2. Run 'git wtclean -d' to remove, or 'git wtclean -D' to force remove.",
		"",
	}, "\n")
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}

	if got := runner.callsContaining("worktree remove"); len(got) != 0 {
		t.Fatalf("remove calls = %#v, want none", got)
	}
}

func TestRunDeleteRemovesWithoutForce(t *testing.T) {
	app, runner, _, stderr := newTestApp()

	code := app.Run(context.Background(), []string{"-d"})

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}

	want := []string{
		"git -C /tmp/repo1 worktree remove /tmp/repo1/.wt/feature-a",
		"git -C /tmp/repo2 worktree remove /tmp/repo2/.wt/feature-b",
	}
	if got := runner.callsContaining("worktree remove"); !reflect.DeepEqual(got, want) {
		t.Fatalf("remove calls = %#v, want %#v", got, want)
	}
}

func TestRunForceDeleteRemovesWithForce(t *testing.T) {
	app, runner, _, stderr := newTestApp()

	code := app.Run(context.Background(), []string{"-D"})

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}

	want := []string{
		"git -C /tmp/repo1 worktree remove --force /tmp/repo1/.wt/feature-a",
		"git -C /tmp/repo2 worktree remove --force /tmp/repo2/.wt/feature-b",
	}
	if got := runner.callsContaining("worktree remove"); !reflect.DeepEqual(got, want) {
		t.Fatalf("remove calls = %#v, want %#v", got, want)
	}
}

func TestRunPruneQuietByDefault(t *testing.T) {
	app, runner, stdout, stderr := newTestApp()

	code := app.Run(context.Background(), []string{"--prune"})

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}

	want := strings.Join([]string{
		"Done. prune_checked=2 failed=0",
		"",
	}, "\n")
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}

	if got := runner.callsContaining("worktree prune"); len(got) != 2 {
		t.Fatalf("prune call count = %d, want 2: %#v", len(got), got)
	}

	if got := runner.callsContaining("worktree list"); len(got) != 0 {
		t.Fatalf("worktree list calls = %#v, want none", got)
	}
}

func TestRunPruneVerbosePrintsRepositories(t *testing.T) {
	app, _, stdout, stderr := newTestApp()

	code := app.Run(context.Background(), []string{"--prune", "--verbose"})

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr.String())
	}

	want := strings.Join([]string{
		"Pruning: /tmp/repo1",
		"Pruning: /tmp/repo2",
		"Done. prune_checked=2 failed=0",
		"",
	}, "\n")
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunRejectsMultipleDeleteOptions(t *testing.T) {
	app, _, _, stderr := newTestApp()

	code := app.Run(context.Background(), []string{"-d", "-D"})

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "specify only one delete option") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func newTestApp() (*App, *fakeRunner, *bytes.Buffer, *bytes.Buffer) {
	runner := &fakeRunner{}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	return NewApp(runner, &stdout, &stderr), runner, &stdout, &stderr
}

type fakeRunner struct {
	calls []string
}

func (r *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, call)

	switch call {
	case "ghq list -p":
		return []byte("/tmp/repo1\n/tmp/repo2\n/tmp/not-git\n"), nil
	case "git -C /tmp/repo1 rev-parse --git-dir", "git -C /tmp/repo2 rev-parse --git-dir":
		return []byte(".git\n"), nil
	case "git -C /tmp/not-git rev-parse --git-dir":
		return nil, errors.New("not a git repository")
	case "git -C /tmp/repo1 worktree list --porcelain -z":
		return porcelain(
			[]string{"worktree /tmp/repo1", "HEAD 1111111111111111111111111111111111111111", "branch refs/heads/main"},
			[]string{"worktree /tmp/repo1/.wt/feature-a", "HEAD 2222222222222222222222222222222222222222", "branch refs/heads/feature-a"},
			[]string{"worktree /tmp/repo1/.wt/bare-cache", "bare"},
		), nil
	case "git -C /tmp/repo2 worktree list --porcelain -z":
		return porcelain(
			[]string{"worktree /tmp/repo2", "HEAD 3333333333333333333333333333333333333333", "branch refs/heads/main"},
			[]string{"worktree /tmp/repo2/.wt/feature-b", "HEAD 4444444444444444444444444444444444444444", "branch refs/heads/feature-b"},
		), nil
	case "git -C /tmp/repo1 worktree remove /tmp/repo1/.wt/feature-a",
		"git -C /tmp/repo2 worktree remove /tmp/repo2/.wt/feature-b",
		"git -C /tmp/repo1 worktree remove --force /tmp/repo1/.wt/feature-a",
		"git -C /tmp/repo2 worktree remove --force /tmp/repo2/.wt/feature-b",
		"git -C /tmp/repo1 worktree prune",
		"git -C /tmp/repo2 worktree prune":
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected command: %s", call)
	}
}

func (r *fakeRunner) callsContaining(substr string) []string {
	var calls []string
	for _, call := range r.calls {
		if strings.Contains(call, substr) {
			calls = append(calls, call)
		}
	}
	return calls
}

func porcelain(entries ...[]string) []byte {
	var out []byte
	for _, entry := range entries {
		for _, field := range entry {
			out = append(out, field...)
			out = append(out, 0)
		}
		out = append(out, 0)
	}
	return out
}
