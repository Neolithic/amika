package sandboxcmd

// sandbox_create_git.go prepares git-backed sandbox mounts and branch state.

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gofixpoint/amika/internal/sandbox"
)

type gitMountInfo struct {
	RepoName string
	RepoRoot string
	NoClean  bool
	Mount    sandbox.MountBinding
}

func prepareGitMount(startPath string, noClean bool, cloneFn func(src, dst string) error, branch, newBranch string) (gitMountInfo, func(), error) {
	repoRoot, err := resolveGitRoot(startPath)
	if err != nil {
		return gitMountInfo{}, func() {}, err
	}

	repoName := filepath.Base(repoRoot)
	target := path.Join(sandbox.SandboxWorkdir, repoName)
	tmpDir, err := os.MkdirTemp("", "amika-git-mount-*")
	if err != nil {
		return gitMountInfo{}, func() {}, fmt.Errorf("failed to create temp directory for git mount: %w", err)
	}
	preparedRepo := filepath.Join(tmpDir, repoName)
	if noClean {
		if err := copyRepoWorkingTree(repoRoot, preparedRepo); err != nil {
			_ = os.RemoveAll(tmpDir)
			return gitMountInfo{}, func() {}, err
		}
	} else {
		if err := cloneFn(repoRoot, preparedRepo); err != nil {
			_ = os.RemoveAll(tmpDir)
			return gitMountInfo{}, func() {}, err
		}
	}
	if err := applyBranchCheckout(preparedRepo, branch, newBranch); err != nil {
		_ = os.RemoveAll(tmpDir)
		return gitMountInfo{}, func() {}, err
	}
	if err := syncGitRemotes(repoRoot, preparedRepo); err != nil {
		_ = os.RemoveAll(tmpDir)
		return gitMountInfo{}, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	return gitMountInfo{
		RepoName: repoName,
		RepoRoot: repoRoot,
		NoClean:  noClean,
		Mount: sandbox.MountBinding{
			Type:         "bind",
			Source:       preparedRepo,
			Target:       target,
			Mode:         "rwcopy",
			SnapshotFrom: repoRoot,
		},
	}, cleanup, nil
}

func resolveGitRoot(startPath string) (string, error) {
	if startPath == "" {
		startPath = "."
	}
	absPath, err := filepath.Abs(startPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve git start path %q: %w", startPath, err)
	}

	current := absPath
	if stat, err := os.Stat(absPath); err == nil && !stat.IsDir() {
		current = filepath.Dir(absPath)
	}

	for {
		gitMarker := filepath.Join(current, ".git")
		if _, err := os.Stat(gitMarker); err == nil {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return "", fmt.Errorf("no git repository root found from %q", absPath)
}

func cloneGitRepo(src, dst string) error {
	args := []string{"clone", "--local", "--no-hardlinks", src, dst}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to prepare clean git mount from %q: %s", src, strings.TrimSpace(string(out)))
	}
	return nil
}

func cloneGitURL(src, dst string) error {
	cmd := exec.Command("git", "clone", src, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to clone %q: %s", src, strings.TrimSpace(string(out)))
	}
	return nil
}

// prepareGitMountFromURL clones a remote URL into a temporary directory and
// returns a mount that exposes the cloned tree to the sandbox.
func prepareGitMountFromURL(rawURL string, cloneFn func(src, dst string) error, branch, newBranch string) (gitMountInfo, func(), error) {
	name, err := repoNameFromURL(rawURL)
	if err != nil {
		return gitMountInfo{}, func() {}, err
	}
	target := path.Join(sandbox.SandboxWorkdir, name)
	tmpDir, err := os.MkdirTemp("", "amika-git-mount-*")
	if err != nil {
		return gitMountInfo{}, func() {}, fmt.Errorf("failed to create temp directory for git mount: %w", err)
	}
	preparedRepo := filepath.Join(tmpDir, name)
	if err := cloneFn(rawURL, preparedRepo); err != nil {
		_ = os.RemoveAll(tmpDir)
		return gitMountInfo{}, func() {}, err
	}
	if err := applyBranchCheckout(preparedRepo, branch, newBranch); err != nil {
		_ = os.RemoveAll(tmpDir)
		return gitMountInfo{}, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }
	return gitMountInfo{
		RepoName: name,
		RepoRoot: rawURL,
		NoClean:  false,
		Mount: sandbox.MountBinding{
			Type:         "bind",
			Source:       preparedRepo,
			Target:       target,
			Mode:         "rwcopy",
			SnapshotFrom: rawURL,
		},
	}, cleanup, nil
}

func branchOrRemoteExists(repoDir, branch string) bool {
	for _, ref := range []string{"refs/heads/" + branch, "refs/remotes/origin/" + branch} {
		cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "--quiet", ref)
		if err := cmd.Run(); err == nil {
			return true
		}
	}
	return false
}

func localBranchExists(repoDir, branch string) bool {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

func remoteTrackingBranchExists(repoDir, branch string) bool {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	return cmd.Run() == nil
}

func detectDefaultBranch(repoDir string) (string, error) {
	for _, b := range []string{"main", "master"} {
		if branchOrRemoteExists(repoDir, b) {
			return b, nil
		}
	}
	return "", fmt.Errorf("could not locate 'main' or 'master' branch; specify --branch explicitly")
}

func runGitInDir(dir string, args ...string) error {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

func checkoutPreparedBranch(repoDir, branch string) error {
	switch {
	case localBranchExists(repoDir, branch):
		if err := runGitInDir(repoDir, "checkout", branch); err != nil {
			return fmt.Errorf("failed to checkout base branch %q: %w", branch, err)
		}
	case remoteTrackingBranchExists(repoDir, branch):
		if err := runGitInDir(repoDir, "checkout", "-B", branch, "refs/remotes/origin/"+branch); err != nil {
			return fmt.Errorf("failed to checkout base branch %q from origin/%s: %w", branch, branch, err)
		}
	default:
		return fmt.Errorf("base branch %q does not exist in the repository", branch)
	}
	return nil
}

func applyBranchCheckout(repoDir, branch, newBranch string) error {
	if newBranch != "" && branch == "" {
		if err := runGitInDir(repoDir, "checkout", "-b", newBranch); err != nil {
			return fmt.Errorf("failed to create branch %q: %w", newBranch, err)
		}
		return nil
	}

	if branch != "" {
		if branchOrRemoteExists(repoDir, branch) {
			if err := checkoutPreparedBranch(repoDir, branch); err != nil {
				return err
			}
		} else {
			if err := runGitInDir(repoDir, "checkout", "-b", branch); err != nil {
				return fmt.Errorf("failed to create branch %q: %w", branch, err)
			}
		}
	}

	if newBranch != "" {
		if err := runGitInDir(repoDir, "checkout", "-b", newBranch); err != nil {
			return fmt.Errorf("failed to create branch %q: %w", newBranch, err)
		}
	}
	return nil
}

func detectHostCurrentBranch(startPath string) (string, error) {
	repoRoot, err := resolveGitRoot(startPath)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "-C", repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to detect current host branch: %w", err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" || name == "HEAD" {
		return "", fmt.Errorf("detached HEAD; specify --branch explicitly")
	}
	return name, nil
}

// isLocalBranchReachableFromRemote returns true when the local branch tip
// is an ancestor of (or equal to) the corresponding branch on the "origin"
// remote. This means the remote already contains every commit on the local
// branch, so it is safe to create a sandbox from the remote version.
//
// It always checks against origin directly (not the upstream tracking
// branch) because sandbox creation resolves the origin URL regardless of
// what remote the branch tracks.
//
// The ancestry check uses "git merge-base --is-ancestor", which requires
// both SHAs to be in the local object store. The remote SHA (obtained via
// ls-remote) may not be local if the user hasn't fetched recently — the
// common case when someone else pushes to the branch. To avoid a fetch
// (which would mutate local state), we fall back to comparing against the
// last-fetched tracking ref (refs/remotes/origin/<branch>). If local
// hasn't moved past that ref, and the remote is even further ahead, then
// local is certainly behind remote and it is safe to proceed.
func isLocalBranchReachableFromRemote(repoDir, branch string) bool {
	// Query origin for the branch tip SHA without downloading objects.
	lsCmd := exec.Command("git", "-C", repoDir, "ls-remote", "--heads", "origin", branch)
	lsOut, err := lsCmd.Output()
	if err != nil || strings.TrimSpace(string(lsOut)) == "" {
		return false // branch doesn't exist on origin
	}
	remoteSHA := strings.Fields(strings.TrimSpace(string(lsOut)))[0]

	// Get the local branch tip SHA.
	localCmd := exec.Command("git", "-C", repoDir, "rev-parse", branch)
	localOut, err := localCmd.Output()
	if err != nil {
		return false
	}
	localSHA := strings.TrimSpace(string(localOut))

	// Fast path: tips match exactly.
	if remoteSHA == localSHA {
		return true
	}

	// Check whether the remote SHA exists in the local object store. It
	// will be present if the user has fetched recently, or if the commit
	// was created locally and then pushed.
	catCmd := exec.Command("git", "-C", repoDir, "cat-file", "-e", remoteSHA)
	if catCmd.Run() == nil {
		// Remote SHA is local — do a precise ancestry check.
		// "merge-base --is-ancestor A B" exits 0 when A is an ancestor of B,
		// meaning the remote (B) contains every commit in local (A).
		ancestorCmd := exec.Command("git", "-C", repoDir, "merge-base", "--is-ancestor", localSHA, remoteSHA)
		return ancestorCmd.Run() == nil
	}

	// Remote SHA is NOT in the local object store (e.g. someone else pushed
	// new commits and we haven't fetched). Fall back to the last-fetched
	// tracking ref: if local hasn't moved past origin/<branch>, then local
	// has no unpushed commits and must be behind the (even newer) remote.
	trackingRef := "refs/remotes/origin/" + branch
	verifyCmd := exec.Command("git", "-C", repoDir, "rev-parse", "--verify", "--quiet", trackingRef)
	if verifyCmd.Run() != nil {
		return false // no tracking ref — can't determine relationship
	}
	trackingAncestorCmd := exec.Command("git", "-C", repoDir, "merge-base", "--is-ancestor", localSHA, trackingRef)
	return trackingAncestorCmd.Run() == nil
}

func copyRepoWorkingTree(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("failed to create no-clean parent for %q: %w", dst, err)
	}
	cmd := exec.Command("cp", "-a", src, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to prepare no-clean git mount from %q: %s", src, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err != nil {
		return fmt.Errorf("failed to prepare no-clean git mount from %q: missing .git in %q", src, dst)
	}
	return nil
}

func syncGitRemotes(srcRepo, dstRepo string) error {
	srcRemotes, err := listGitRemotes(srcRepo)
	if err != nil {
		return fmt.Errorf("failed to read remotes from source repo %q: %w", srcRepo, err)
	}
	filtered := make(map[string]string)
	for name, url := range srcRemotes {
		if isNetworkRemoteURL(url) {
			filtered[name] = url
		}
	}

	dstRemotes, err := listGitRemotes(dstRepo)
	if err != nil {
		return fmt.Errorf("failed to read remotes from prepared repo %q: %w", dstRepo, err)
	}
	for _, name := range sortedRemoteNames(dstRemotes) {
		if err := runGit(dstRepo, "remote", "remove", name); err != nil {
			return fmt.Errorf("failed to remove remote %q from prepared repo %q: %w", name, dstRepo, err)
		}
	}
	for _, name := range sortedRemoteNames(filtered) {
		if err := runGit(dstRepo, "remote", "add", name, filtered[name]); err != nil {
			return fmt.Errorf("failed to add remote %q to prepared repo %q: %w", name, dstRepo, err)
		}
	}
	return nil
}

func listGitRemotes(repo string) (map[string]string, error) {
	out, err := runGitOutput(repo, "remote")
	if err != nil {
		return nil, err
	}
	names := strings.Fields(strings.TrimSpace(out))
	remotes := make(map[string]string, len(names))
	for _, name := range names {
		url, err := runGitOutput(repo, "remote", "get-url", name)
		if err != nil {
			return nil, err
		}
		remotes[name] = strings.TrimSpace(url)
	}
	return remotes, nil
}

func isNetworkRemoteURL(url string) bool {
	switch {
	case strings.HasPrefix(url, "http://"),
		strings.HasPrefix(url, "https://"),
		strings.HasPrefix(url, "ssh://"):
		return true
	case strings.HasPrefix(url, "file://"):
		return false
	}
	at := strings.Index(url, "@")
	colon := strings.Index(url, ":")
	return at > 0 && colon > at+1
}

func sortedRemoteNames(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func runGit(repo string, args ...string) error {
	_, err := runGitOutput(repo, args...)
	return err
}

func runGitOutput(repo string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

type repoSource int

const (
	repoSourceNone repoSource = iota
	repoSourceAutoDetect
	repoSourceFlagPath
	repoSourceFlagURL
)

type repoIdentity struct {
	Name   string
	Source repoSource
	Path   string
	URL    string
}

// resolveRepoIdentity decides which git repo (if any) should back a sandbox
// based on the user's flag input and the working directory.
//
//   - --git and --no-git are mutually exclusive.
//   - --no-clean and --no-git are mutually exclusive.
//   - --no-clean is only meaningful when a local-path repo is sourced
//     (auto-detect or --git <path>).
//   - When neither --git nor --no-git is set, the function walks up from cwd
//     to detect a repo; if none is found, identity is repoSourceNone.
func resolveRepoIdentity(cwd, gitFlag string, gitFlagSet, noGit, noClean bool) (repoIdentity, error) {
	if gitFlagSet && noGit {
		return repoIdentity{}, fmt.Errorf("--git and --no-git are mutually exclusive")
	}
	if noClean && noGit {
		return repoIdentity{}, fmt.Errorf("--no-clean and --no-git are mutually exclusive")
	}
	if noGit {
		return repoIdentity{Source: repoSourceNone}, nil
	}
	if gitFlagSet {
		v := strings.TrimSpace(gitFlag)
		if v == "" {
			return repoIdentity{}, fmt.Errorf("--git requires a non-empty value")
		}
		if isNetworkRemoteURL(v) {
			if noClean {
				return repoIdentity{}, fmt.Errorf("--no-clean cannot be used with a git URL")
			}
			name, err := repoNameFromURL(v)
			if err != nil {
				return repoIdentity{}, err
			}
			return repoIdentity{Name: name, Source: repoSourceFlagURL, URL: v}, nil
		}
		repoRoot, err := resolveGitRoot(v)
		if err != nil {
			return repoIdentity{}, fmt.Errorf("could not find git repo at %q: %w", v, err)
		}
		return repoIdentity{Name: filepath.Base(repoRoot), Source: repoSourceFlagPath, Path: repoRoot}, nil
	}
	repoRoot, err := resolveGitRoot(cwd)
	if err != nil {
		if noClean {
			return repoIdentity{}, fmt.Errorf("--no-clean requires a git repo, but none was detected from %q", cwd)
		}
		return repoIdentity{Source: repoSourceNone}, nil
	}
	return repoIdentity{Name: filepath.Base(repoRoot), Source: repoSourceAutoDetect, Path: repoRoot}, nil
}

// repoNameFromURL extracts the repo name from a git URL.
// Examples:
//
//	https://github.com/foo/bar       -> bar
//	https://github.com/foo/bar.git   -> bar
//	git@github.com:foo/bar.git       -> bar
//	ssh://git@github.com/foo/bar.git -> bar
func repoNameFromURL(rawURL string) (string, error) {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return "", fmt.Errorf("empty git URL")
	}
	var pathPart string
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", fmt.Errorf("parsing git URL %q: %w", rawURL, err)
		}
		pathPart = u.Path
	} else if i := strings.Index(s, ":"); i >= 0 {
		pathPart = s[i+1:]
	} else {
		return "", fmt.Errorf("not a git URL: %q", rawURL)
	}
	pathPart = strings.TrimSuffix(strings.TrimSpace(pathPart), "/")
	if pathPart == "" {
		return "", fmt.Errorf("git URL %q has no repo path", rawURL)
	}
	if i := strings.LastIndex(pathPart, "/"); i >= 0 {
		pathPart = pathPart[i+1:]
	}
	pathPart = strings.TrimSuffix(pathPart, ".git")
	if pathPart == "" {
		return "", fmt.Errorf("could not extract repo name from %q", rawURL)
	}
	return pathPart, nil
}

func resolveGitURL(value string) (string, error) {
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "git@") {
		return value, nil
	}

	repoRoot, err := resolveGitRoot(value)
	if err != nil {
		return "", fmt.Errorf("could not find git repo at %q: %w", value, err)
	}
	remotes, err := listGitRemotes(repoRoot)
	if err != nil {
		return "", err
	}
	origin, ok := remotes["origin"]
	if !ok {
		return "", fmt.Errorf("no origin remote found in %q; specify a git HTTP(S) or SSH URL directly with --git <url>, or pass --no-git to create a sandbox without a repo", repoRoot)
	}
	if !isNetworkRemoteURL(origin) {
		return "", fmt.Errorf("origin remote %q is a local path; specify a git HTTP(S) or SSH URL directly with --git <url>, or pass --no-git to create a sandbox without a repo", origin)
	}
	return origin, nil
}
