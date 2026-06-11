package eventlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// fakeUploader records object keys (and bytes) it is asked to upload, and can
// be told to fail for specific keys.
type fakeUploader struct {
	uploaded map[string][]byte
	failKeys map[string]bool
}

func newFakeUploader() *fakeUploader {
	return &fakeUploader{uploaded: map[string][]byte{}, failKeys: map[string]bool{}}
}

func (f *fakeUploader) Upload(objectKey string, data []byte) error {
	if f.failKeys[objectKey] {
		return fmt.Errorf("forced failure for %s", objectKey)
	}
	f.uploaded[objectKey] = data
	return nil
}

func (f *fakeUploader) keys() []string {
	ks := make([]string, 0, len(f.uploaded))
	for k := range f.uploaded {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// writeTestEvent writes one event JSON file into the on-disk layout under
// stateDir, returning the file path.
func writeTestEvent(t *testing.T, stateDir string, src Source, sessionDir string, seq int, git *GitInfo) string {
	t.Helper()
	dir := filepath.Join(EventsDir(stateDir, src), sessionDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ev := Event{
		Source:    src,
		HookEvent: "PostToolUse",
		SessionID: "sess",
		Seq:       seq,
		Git:       git,
		Payload:   json.RawMessage(`{}`),
	}
	data, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("event_%d_20240101T000000.000000000Z.json", seq))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestPush_UploadsWithRepoPrefixedKeys(t *testing.T) {
	stateDir := t.TempDir()
	writeTestEvent(t, stateDir, SourceClaude, "20240101T000000.000000000Z_sess-a", 0, &GitInfo{RepoRoot: "/home/u/work/amika"})
	writeTestEvent(t, stateDir, SourceClaude, "20240101T000000.000000000Z_sess-a", 1, &GitInfo{RepoRoot: "/home/u/work/amika"})
	// A codex session with no git context falls back to the unknown-repo prefix.
	writeTestEvent(t, stateDir, SourceCodex, "20240101T000000.000000000Z_sess-b", 0, nil)

	up := newFakeUploader()
	report, err := Push(stateDir, up)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if report.Uploaded != 3 || report.Skipped != 0 || report.Failed != 0 {
		t.Fatalf("report = %+v, want uploaded=3 skipped=0 failed=0", report)
	}

	// On-disk names are uppercase, but object keys are lowercased at upload so
	// they round-trip through case-folding storage listings.
	want := []string{
		"amika/claude/sessions/20240101t000000.000000000z_sess-a/event_0_20240101t000000.000000000z.json",
		"amika/claude/sessions/20240101t000000.000000000z_sess-a/event_1_20240101t000000.000000000z.json",
		"unknown-repo/codex/sessions/20240101t000000.000000000z_sess-b/event_0_20240101t000000.000000000z.json",
	}
	got := up.keys()
	if len(got) != len(want) {
		t.Fatalf("uploaded keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPush_SecondRunSkipsAlreadyUploaded(t *testing.T) {
	stateDir := t.TempDir()
	writeTestEvent(t, stateDir, SourceClaude, "20240101T000000.000000000Z_sess-a", 0, &GitInfo{RepoRoot: "/x/amika"})

	first := newFakeUploader()
	r1, err := Push(stateDir, first)
	if err != nil {
		t.Fatalf("Push #1: %v", err)
	}
	if r1.Uploaded != 1 {
		t.Fatalf("first run uploaded = %d, want 1", r1.Uploaded)
	}

	// A new file appears; the second run uploads only that one.
	writeTestEvent(t, stateDir, SourceClaude, "20240101T000000.000000000Z_sess-a", 1, &GitInfo{RepoRoot: "/x/amika"})
	second := newFakeUploader()
	r2, err := Push(stateDir, second)
	if err != nil {
		t.Fatalf("Push #2: %v", err)
	}
	if r2.Uploaded != 1 || r2.Skipped != 1 {
		t.Fatalf("second run = %+v, want uploaded=1 skipped=1", r2)
	}
	if len(second.uploaded) != 1 {
		t.Errorf("second run uploaded %d objects, want 1", len(second.uploaded))
	}
}

func TestPush_FailedUploadIsNotMarkedDone(t *testing.T) {
	stateDir := t.TempDir()
	writeTestEvent(t, stateDir, SourceClaude, "20240101T000000.000000000Z_sess-a", 0, &GitInfo{RepoRoot: "/x/amika"})
	key := "amika/claude/sessions/20240101t000000.000000000z_sess-a/event_0_20240101t000000.000000000z.json"

	failing := newFakeUploader()
	failing.failKeys[key] = true
	r1, err := Push(stateDir, failing)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if r1.Failed != 1 || r1.Uploaded != 0 {
		t.Fatalf("report = %+v, want failed=1 uploaded=0", r1)
	}

	// A subsequent successful run retries the previously-failed file.
	ok := newFakeUploader()
	r2, err := Push(stateDir, ok)
	if err != nil {
		t.Fatalf("Push retry: %v", err)
	}
	if r2.Uploaded != 1 || r2.Skipped != 0 {
		t.Fatalf("retry report = %+v, want uploaded=1 skipped=0", r2)
	}
}

func TestPush_NoEventsIsNoOp(t *testing.T) {
	report, err := Push(t.TempDir(), newFakeUploader())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if report.Uploaded != 0 || report.Skipped != 0 || report.Failed != 0 {
		t.Fatalf("report = %+v, want all zero", report)
	}
}
