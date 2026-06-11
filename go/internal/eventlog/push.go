package eventlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Uploader uploads one object's bytes under the given object key. The key uses
// forward slashes and is a relative path within the destination bucket.
type Uploader interface {
	Upload(objectKey string, data []byte) error
}

// PushReport summarizes a Push run.
type PushReport struct {
	// Uploaded is the number of event files uploaded this run.
	Uploaded int
	// Skipped is the number of event files already recorded in the manifest.
	Skipped int
	// Failed is the number of event files whose upload returned an error.
	Failed int
	// Errors holds one error per failed file (parallel to Failed).
	Errors []error
}

// pushManifestName is the file under <state>/events that records which event
// files have already been uploaded, so repeated pushes are incremental.
const pushManifestName = ".amikalog-push-state.json"

// unknownRepoSegment is the object-key prefix used for a session whose events
// carry no git repository context.
const unknownRepoSegment = "unknown-repo"

// pushManifest tracks uploaded event files keyed by their path relative to
// <state>/events (e.g. "claude/sessions/<dir>/event_0_<ts>.json"); the value
// is the RFC3339 time the upload succeeded.
type pushManifest struct {
	Uploaded map[string]string `json:"uploaded"`
}

// Push uploads every not-yet-pushed event file under stateDir to up, recording
// successes in a local manifest so subsequent runs only send new files.
//
// The object key for each file is its path relative to <state>/events prefixed
// with the file's repository, i.e.
// "<repo>/<source>/sessions/<session-dir>/<event-file>.json". Each session is
// scoped to a single repository, so the repo segment is resolved once per
// session directory from the captured git.repo_root (basename), falling back to
// "unknown-repo" when no git context was recorded.
//
// Per-file upload failures are collected in the report and do not abort the
// run; a non-nil error is returned only for failures that prevent the walk
// itself (e.g. an unreadable manifest).
func Push(stateDir string, up Uploader) (PushReport, error) {
	eventsBase := filepath.Join(stateDir, "events")
	manifestPath := filepath.Join(eventsBase, pushManifestName)

	manifest, err := loadPushManifest(manifestPath)
	if err != nil {
		return PushReport{}, err
	}

	var report PushReport
	for _, src := range []Source{SourceClaude, SourceCodex} {
		sessionsRoot := EventsDir(stateDir, src)
		sessions, err := os.ReadDir(sessionsRoot)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return report, fmt.Errorf("reading %s sessions: %w", src, err)
		}
		for _, session := range sessions {
			if !session.IsDir() {
				continue
			}
			sessionDir := filepath.Join(sessionsRoot, session.Name())
			repoSeg := resolveRepoSegment(sessionDir)
			if err := pushSession(up, manifest, manifestPath, eventsBase, sessionDir, repoSeg, &report); err != nil {
				return report, err
			}
		}
	}
	return report, nil
}

// pushSession uploads the not-yet-pushed event files in one session directory.
func pushSession(up Uploader, manifest *pushManifest, manifestPath, eventsBase, sessionDir, repoSeg string, report *PushReport) error {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return fmt.Errorf("reading session dir %s: %w", sessionDir, err)
	}
	// Sort so uploads (and the manifest) follow a stable, sequence-ordered path.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "event_") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		filePath := filepath.Join(sessionDir, name)
		relPath, err := filepath.Rel(eventsBase, filePath)
		if err != nil {
			return fmt.Errorf("relativizing %s: %w", filePath, err)
		}
		relKey := filepath.ToSlash(relPath)
		if _, done := manifest.Uploaded[relKey]; done {
			report.Skipped++
			continue
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			report.Failed++
			report.Errors = append(report.Errors, fmt.Errorf("reading %s: %w", relKey, err))
			continue
		}
		// Lowercase the object key: storage listings fold case in directory
		// segments, so an uppercase key can be listed but not signed back during
		// beta:fetch. Normalizing here covers every event file regardless of its
		// on-disk filename casing.
		objectKey := strings.ToLower(path.Join(repoSeg, relKey))
		if err := up.Upload(objectKey, data); err != nil {
			report.Failed++
			report.Errors = append(report.Errors, fmt.Errorf("uploading %s: %w", objectKey, err))
			continue
		}
		manifest.Uploaded[relKey] = time.Now().UTC().Format(time.RFC3339)
		// Persist after each success so an interrupted run resumes cleanly.
		if err := savePushManifest(manifestPath, manifest); err != nil {
			return err
		}
		report.Uploaded++
	}
	return nil
}

// resolveRepoSegment returns the sanitized repository basename for a session,
// read from the first event file that carries git context. It returns
// "unknown-repo" when the session has no event with a git repo root.
func resolveRepoSegment(sessionDir string) string {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return unknownRepoSegment
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "event_") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(sessionDir, name))
		if err != nil {
			continue
		}
		var ev Event
		if json.Unmarshal(data, &ev) != nil {
			continue
		}
		if ev.Git != nil && ev.Git.RepoRoot != "" {
			return sanitizeRepoSegment(filepath.Base(ev.Git.RepoRoot))
		}
	}
	return unknownRepoSegment
}

// sanitizeRepoSegment makes a repository name safe as a single object-key
// segment, replacing path separators, whitespace, and the URL delimiters the
// upload endpoint rejects. Empty input becomes "unknown-repo".
func sanitizeRepoSegment(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return unknownRepoSegment
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ' ', '\t', '\n', '\r', '?', '#', '%':
			return '-'
		}
		return r
	}, name)
}

// loadPushManifest reads the manifest, returning an empty one when the file
// does not yet exist.
func loadPushManifest(path string) (*pushManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &pushManifest{Uploaded: map[string]string{}}, nil
		}
		return nil, fmt.Errorf("reading push manifest %s: %w", path, err)
	}
	var m pushManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing push manifest %s: %w", path, err)
	}
	if m.Uploaded == nil {
		m.Uploaded = map[string]string{}
	}
	return &m, nil
}

// savePushManifest writes the manifest atomically (write-then-rename) so an
// interrupted write cannot corrupt the existing manifest.
func savePushManifest(path string, m *pushManifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating manifest dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing push manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replacing push manifest: %w", err)
	}
	return nil
}
