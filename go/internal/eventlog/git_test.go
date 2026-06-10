package eventlog

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGatherGit_NotARepo(t *testing.T) {
	if got := GatherGit(t.TempDir()); got != nil {
		t.Fatalf("GatherGit(non-repo) = %+v, want nil", got)
	}
	if got := GatherGit(""); got != nil {
		t.Fatalf("GatherGit(\"\") = %+v, want nil", got)
	}
}

func TestGatherGit_CommitBranchDirty(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	initRepo(t, dir)
	writeFile(t, filepath.Join(dir, "a.txt"), "hello")
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "initial")

	got := GatherGit(dir)
	if got == nil {
		t.Fatal("GatherGit(repo) = nil, want info")
	}
	// RepoRoot may be reported via a symlink-resolved path (e.g. /private on
	// macOS), so compare basenames rather than the full path.
	if filepath.Base(got.RepoRoot) != filepath.Base(dir) {
		t.Errorf("RepoRoot = %q, want basename %q", got.RepoRoot, filepath.Base(dir))
	}
	if len(got.Commit) != 40 {
		t.Errorf("Commit = %q, want a 40-char sha", got.Commit)
	}
	if got.Branch != "main" {
		t.Errorf("Branch = %q, want main", got.Branch)
	}
	if got.Dirty {
		t.Error("Dirty = true on a clean tree, want false")
	}

	writeFile(t, filepath.Join(dir, "untracked.txt"), "x")
	if got := GatherGit(dir); got == nil || !got.Dirty {
		t.Errorf("Dirty = %v, want true after adding untracked file", got)
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

// initRepo creates a git repo with a deterministic default branch and an
// isolated config so the host's global git settings cannot affect the test.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "nonexistent-global"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "nonexistent-system"))
	runGit(t, dir, "-c", "init.defaultBranch=main", "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "commit.gpgsign", "false")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
