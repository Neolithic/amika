package eventlog

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// gitTimeout bounds each git invocation so a wedged git process can never hang
// an agent's hook.
const gitTimeout = 5 * time.Second

// GatherGit returns the git state of dir, or nil when dir is not inside a git
// repository or git is unavailable. It never returns an error: capturing the
// event must not fail just because git context could not be collected.
func GatherGit(dir string) *GitInfo {
	if dir == "" {
		return nil
	}
	root, ok := gitOutput(dir, "rev-parse", "--show-toplevel")
	if !ok || root == "" {
		return nil
	}
	// HEAD lookups fail in a repository with no commits yet; that is fine, we
	// still record the repo root and dirty state with an empty commit/branch.
	commit, _ := gitOutput(dir, "rev-parse", "HEAD")
	branch, _ := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	status, _ := gitOutput(dir, "status", "--porcelain")
	return &GitInfo{
		RepoRoot: root,
		Commit:   commit,
		Branch:   branch,
		Dirty:    strings.TrimSpace(status) != "",
	}
}

// gitOutput runs `git -C dir <args...>` and returns its trimmed stdout. The
// boolean reports success; callers treat failure as "information unavailable".
func gitOutput(dir string, args ...string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
