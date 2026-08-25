package repository

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"karst-map-release/internal/domain"
)

const schemaVersion = 1

var ErrVersionConflict = errors.New("expectedVersion 与当前版本不一致")

type Store interface {
	Get(packageID string) (*domain.SurveyPackage, error)
	GetIdempotency(key string) (json.RawMessage, bool)
	Commit(packageID string, expectedVersion int64, eventType string, aggregate *domain.SurveyPackage, idempotencyKey string, result json.RawMessage) error
	Health() (HealthReport, error)
	Close() error
}

type HealthReport struct {
	SchemaVersion int    `json:"schemaVersion"`
	EventSequence int64  `json:"eventSequence"`
	PackageCount  int    `json:"packageCount"`
	Integrity     string `json:"integrity"`
}

type EventEnvelope struct {
	SchemaVersion    int                   `json:"schemaVersion"`
	Sequence         int64                 `json:"sequence"`
	PreviousHash     string                `json:"previousHash"`
	Hash             string                `json:"hash"`
	OccurredAt       time.Time             `json:"occurredAt"`
	PackageID        string                `json:"packageId"`
	AggregateVersion int64                 `json:"aggregateVersion"`
	Type             string                `json:"type"`
	Aggregate        *domain.SurveyPackage `json:"aggregate"`
	IdempotencyKey   string                `json:"idempotencyKey,omitempty"`
	Result           json.RawMessage       `json:"result,omitempty"`
}

type snapshot struct {
	SchemaVersion int                              `json:"schemaVersion"`
	LastSequence  int64                            `json:"lastSequence"`
	LastHash      string                           `json:"lastHash"`
	Packages      map[string]*domain.SurveyPackage `json:"packages"`
	Idempotency   map[string]json.RawMessage       `json:"idempotency"`
}

type FileStore struct {
	mu           sync.RWMutex
	dir          string
	logPath      string
	snapshotPath string
	logFile      *os.File
	state        snapshot
}

func Open(dir string) (*FileStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("持久化目录不能为空")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建持久化目录: %w", err)
	}
	s := &FileStore{dir: dir, logPath: filepath.Join(dir, "events.jsonl"), snapshotPath: filepath.Join(dir, "projection.json")}
	s.state = emptySnapshot()
	if err := s.recover(); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("打开事件日志: %w", err)
	}
	s.logFile = f
	return s, nil
}

func emptySnapshot() snapshot {
	return snapshot{SchemaVersion: schemaVersion, Packages: map[string]*domain.SurveyPackage{}, Idempotency: map[string]json.RawMessage{}}
}

func (s *FileStore) recover() error {
	replayed, err := replayLog(s.logPath)
	if err != nil {
		return fmt.Errorf("验证事件日志失败: %w", err)
	}
	onDisk, snapErr := readSnapshot(s.snapshotPath)
	if snapErr == nil && onDisk.LastSequence > replayed.LastSequence {
		return fmt.Errorf("事件日志疑似被截短：快照序号 %d，日志序号 %d", onDisk.LastSequence, replayed.LastSequence)
	}
	if snapErr == nil && snapshotMatches(onDisk, replayed) {
		s.state = onDisk
		return nil
	}
	s.state = replayed
	if err := s.writeSnapshotLocked(); err != nil {
		return fmt.Errorf("重建投影快照: %w", err)
	}
	return nil
}

func replayLog(path string) (snapshot, error) {
	state := emptySnapshot()
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event EventEnvelope
		if err := json.Unmarshal(line, &event); err != nil {
			return state, fmt.Errorf("第 %d 条事件 JSON 无效: %w", state.LastSequence+1, err)
		}
		if event.SchemaVersion != schemaVersion {
			return state, fmt.Errorf("不支持 schemaVersion %d", event.SchemaVersion)
		}
		if event.Sequence != state.LastSequence+1 {
			return state, fmt.Errorf("事件序号不连续: 得到 %d", event.Sequence)
		}
		if event.PreviousHash != state.LastHash {
			return state, fmt.Errorf("事件 %d 前向哈希不匹配", event.Sequence)
		}
		expected, err := eventHash(event)
		if err != nil || expected != event.Hash {
			return state, fmt.Errorf("事件 %d 校验哈希不匹配", event.Sequence)
		}
		if event.Aggregate == nil || event.Aggregate.ID != event.PackageID || event.Aggregate.Version != event.AggregateVersion {
			return state, fmt.Errorf("事件 %d 聚合投影无效", event.Sequence)
		}
		state.Packages[event.PackageID] = event.Aggregate
		if event.IdempotencyKey != "" {
			state.Idempotency[event.IdempotencyKey] = cloneRaw(event.Result)
		}
		state.LastSequence, state.LastHash = event.Sequence, event.Hash
	}
	if err := scanner.Err(); err != nil {
		return state, err
	}
	return state, nil
}

func readSnapshot(path string) (snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return snapshot{}, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 64*1024*1024))
	dec.DisallowUnknownFields()
	var state snapshot
	if err := dec.Decode(&state); err != nil {
		return state, err
	}
	if state.SchemaVersion != schemaVersion || state.Packages == nil || state.Idempotency == nil {
		return state, fmt.Errorf("投影快照结构无效")
	}
	return state, nil
}

func snapshotMatches(a, b snapshot) bool {
	if a.LastSequence != b.LastSequence || a.LastHash != b.LastHash || len(a.Packages) != len(b.Packages) || len(a.Idempotency) != len(b.Idempotency) {
		return false
	}
	for id, p := range a.Packages {
		other, ok := b.Packages[id]
		if !ok || domain.StableDigest(p) != domain.StableDigest(other) {
			return false
		}
	}
	for key, value := range a.Idempotency {
		if !bytes.Equal(value, b.Idempotency[key]) {
			return false
		}
	}
	return true
}

func (s *FileStore) Get(packageID string) (*domain.SurveyPackage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.state.Packages[packageID]
	if !ok {
		return nil, domain.NotFound("成果包")
	}
	return clonePackage(p)
}

func (s *FileStore) GetIdempotency(key string) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.state.Idempotency[key]
	return cloneRaw(value), ok
}

func (s *FileStore) Health() (HealthReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	replayed, err := replayLog(s.logPath)
	if err != nil {
		return HealthReport{}, fmt.Errorf("事件日志完整性检查失败: %w", err)
	}
	if replayed.LastSequence != s.state.LastSequence || replayed.LastHash != s.state.LastHash {
		return HealthReport{}, fmt.Errorf("内存投影与事件日志不一致")
	}
	onDisk, err := readSnapshot(s.snapshotPath)
	if err != nil {
		return HealthReport{}, fmt.Errorf("读取投影快照失败: %w", err)
	}
	if !snapshotMatches(onDisk, replayed) {
		return HealthReport{}, fmt.Errorf("磁盘投影与事件日志不一致")
	}
	return HealthReport{SchemaVersion: schemaVersion, EventSequence: s.state.LastSequence, PackageCount: len(s.state.Packages), Integrity: "verified"}, nil
}

func (s *FileStore) Commit(packageID string, expectedVersion int64, eventType string, aggregate *domain.SurveyPackage, idempotencyKey string, result json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := int64(0)
	if p := s.state.Packages[packageID]; p != nil {
		current = p.Version
	}
	if current != expectedVersion {
		return ErrVersionConflict
	}
	if aggregate == nil || aggregate.ID != packageID || aggregate.Version != expectedVersion+1 {
		return fmt.Errorf("提交聚合版本无效")
	}
	if idempotencyKey == "" {
		return fmt.Errorf("idempotencyKey 不能为空")
	}
	if _, exists := s.state.Idempotency[idempotencyKey]; exists {
		return fmt.Errorf("幂等键已由并发请求提交")
	}
	copyAggregate, err := clonePackage(aggregate)
	if err != nil {
		return err
	}
	event := EventEnvelope{
		SchemaVersion: schemaVersion, Sequence: s.state.LastSequence + 1, PreviousHash: s.state.LastHash,
		OccurredAt: time.Now().UTC(), PackageID: packageID, AggregateVersion: aggregate.Version,
		Type: eventType, Aggregate: copyAggregate, IdempotencyKey: idempotencyKey, Result: cloneRaw(result),
	}
	event.Hash, err = eventHash(event)
	if err != nil {
		return err
	}
	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	written, err := s.logFile.Write(line)
	if err != nil {
		return fmt.Errorf("追加事件: %w", err)
	}
	if written != len(line) {
		return io.ErrShortWrite
	}
	if err := s.logFile.Sync(); err != nil {
		return fmt.Errorf("同步事件日志: %w", err)
	}
	s.state.Packages[packageID] = copyAggregate
	s.state.Idempotency[idempotencyKey] = cloneRaw(result)
	s.state.LastSequence, s.state.LastHash = event.Sequence, event.Hash
	if err := s.writeSnapshotLocked(); err != nil {
		return err
	}
	return nil
}

func (s *FileStore) writeSnapshotLocked() error {
	tmp, err := os.CreateTemp(s.dir, ".projection-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		tmp.Close()
		if !keep {
			os.Remove(tmpName)
		}
	}()
	enc := json.NewEncoder(tmp)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s.state); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Chmod(0o640); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.snapshotPath); err != nil {
		return err
	}
	dir, err := os.Open(s.dir)
	if err == nil {
		err = dir.Sync()
		dir.Close()
	}
	if err != nil {
		return err
	}
	keep = true
	return nil
}

func eventHash(event EventEnvelope) (string, error) {
	event.Hash = ""
	b, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func clonePackage(p *domain.SurveyPackage) (*domain.SurveyPackage, error) {
	if p == nil {
		return nil, fmt.Errorf("成果包不能为空")
	}
	out := *p
	return &out, nil
}

func cloneRaw(in json.RawMessage) json.RawMessage {
	if in == nil {
		return nil
	}
	return append(json.RawMessage(nil), in...)
}

func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.logFile == nil {
		return nil
	}
	err := s.logFile.Close()
	s.logFile = nil
	return err
}
