package log_tail_snapshot_trust_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"karst-map-release/internal/domain"
	"karst-map-release/internal/repository"
)

func TestRestartRejectsTamperedPrefixDespiteValidSnapshotTail(t *testing.T) {
	dir := t.TempDir()
	store, err := repository.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"pkg-prefix", "pkg-tail"} {
		aggregate, err := domain.CreatePackage(domain.NewPackage{
			ID: id, CaveName: "重启校验", SurveyBounds: domain.Bounds{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10},
			CoordinateReferenceSystem: "LOCAL", LayerSummaries: []domain.LayerSummary{{Name: "survey"}}, Owner: "owner",
		}, when)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Commit(id, 0, "package.created", aggregate, "create:"+id, json.RawMessage(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(dir, "events.jsonl")
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(contents), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("预期两条事件，实际为 %d", len(lines))
	}
	var prefix map[string]any
	if err := json.Unmarshal(lines[0], &prefix); err != nil {
		t.Fatal(err)
	}
	prefix["hash"] = strings.Repeat("0", 64)
	lines[0], err = json.Marshal(prefix)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append(bytes.Join(lines, []byte{'\n'}), '\n')
	if err := os.WriteFile(logPath, tampered, 0o640); err != nil {
		t.Fatal(err)
	}

	reopened, err := repository.Open(dir)
	if err == nil {
		reopened.Close()
		t.Fatal("重启必须在复用快照前验证完整事件链")
	}
}
