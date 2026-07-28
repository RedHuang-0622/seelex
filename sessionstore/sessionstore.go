// Package sessionstore provides atomic, project-scoped persistence for chat
// sessions. All backends implement the same logical snapshot contract.
package sessionstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	frameworkStorage "github.com/RedHuang-0622/Seele/seelectx/storage"
	"github.com/RedHuang-0622/Seele/types"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Backend string

const (
	BackendJSON       Backend = "json"
	BackendSQLite     Backend = "sqlite"
	BackendPostgreSQL Backend = "postgres"
)

type Config struct {
	Backend Backend `json:"backend"`
	Path    string  `json:"path,omitempty"`
	DSN     string  `json:"dsn,omitempty"`
}

func (config Config) Normalize(defaultPath string) (Config, error) {
	config.Backend = Backend(strings.ToLower(strings.TrimSpace(string(config.Backend))))
	if config.Backend == "" {
		config.Backend = BackendJSON
	}
	switch config.Backend {
	case BackendJSON:
		if strings.TrimSpace(config.Path) == "" {
			config.Path = filepath.Join(defaultPath, "sessions-json")
		}
	case BackendSQLite:
		if strings.TrimSpace(config.Path) == "" {
			config.Path = filepath.Join(defaultPath, "sessions.db")
		}
	case BackendPostgreSQL:
		if strings.TrimSpace(config.DSN) == "" {
			return Config{}, errors.New("session storage: PostgreSQL DSN is required")
		}
	default:
		return Config{}, fmt.Errorf("session storage: unsupported backend %q", config.Backend)
	}
	return config, nil
}

// Safe returns a configuration suitable for the GUI. Credentials are never
// returned to the renderer; leaving DSN empty means "keep current DSN".
func (config Config) Safe() Config {
	copy := config
	if copy.DSN != "" {
		copy.DSN = "configured"
	}
	return copy
}

type Key struct {
	ProjectID string
	SessionID string
}

func (key Key) validate() error {
	if strings.TrimSpace(key.SessionID) == "" {
		return errors.New("session storage: session ID is required")
	}
	return nil
}

// Repository is the single persistence contract used by session.Manager.
// WriteAtomic replaces one complete logical history. Reads therefore observe
// either the preceding committed history or the next committed history, never
// a mix of shards from both.
type Repository interface {
	WriteAtomic(context.Context, Key, []types.Message) error
	Read(context.Context, Key) ([]types.Message, error)
	ReadRange(context.Context, Key, int, int) ([]types.Message, int, error)
	List(context.Context, string) ([]frameworkStorage.SessionMeta, error)
	Delete(context.Context, Key) error
	Ping(context.Context) error
	Close() error
}

// Router serializes access to the selected backend and supplies the active
// project scope. Changing storage is atomic with respect to all repository
// calls: existing operations finish on the old backend, then future calls use
// the fully initialized replacement.
type Router struct {
	mu          sync.RWMutex
	repository  Repository
	config      Config
	configPath  string
	defaultPath string
	projectID   string
}

func NewRouter(configPath, defaultPath string) (*Router, error) {
	config, err := loadConfig(configPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, fs.ErrNotExist) {
		config = Config{Backend: BackendJSON}
	}
	config, err = config.Normalize(defaultPath)
	if err != nil {
		return nil, err
	}
	repository, err := Open(context.Background(), config)
	if err != nil {
		return nil, err
	}
	return &Router{repository: repository, config: config, configPath: configPath, defaultPath: defaultPath}, nil
}

func (router *Router) SetWorkspace(projectID string) {
	router.mu.Lock()
	router.projectID = strings.TrimSpace(projectID)
	router.mu.Unlock()
}

func (router *Router) Workspace() string {
	router.mu.RLock()
	defer router.mu.RUnlock()
	return router.projectID
}

func (router *Router) Save(sessionID string, messages []types.Message) error {
	return router.withRepository(func(repository Repository, projectID string) error {
		return repository.WriteAtomic(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, messages)
	})
}

func (router *Router) Load(sessionID string) ([]types.Message, error) {
	var messages []types.Message
	err := router.withRepository(func(repository Repository, projectID string) error {
		var err error
		messages, err = repository.Read(context.Background(), Key{ProjectID: projectID, SessionID: sessionID})
		return err
	})
	return messages, err
}

func (router *Router) LoadRange(sessionID string, offset, limit int) ([]types.Message, int, error) {
	var messages []types.Message
	var total int
	err := router.withRepository(func(repository Repository, projectID string) error {
		var err error
		messages, total, err = repository.ReadRange(context.Background(), Key{ProjectID: projectID, SessionID: sessionID}, offset, limit)
		return err
	})
	return messages, total, err
}

func (router *Router) List() []frameworkStorage.SessionMeta {
	var result []frameworkStorage.SessionMeta
	_ = router.withRepository(func(repository Repository, projectID string) error {
		var err error
		result, err = repository.List(context.Background(), projectID)
		return err
	})
	return result
}

func (router *Router) Delete(sessionID string) error {
	return router.withRepository(func(repository Repository, projectID string) error {
		return repository.Delete(context.Background(), Key{ProjectID: projectID, SessionID: sessionID})
	})
}

func (router *Router) MessageCount(sessionID string) (int, error) {
	messages, err := router.Load(sessionID)
	return len(messages), err
}

func (router *Router) Config() Config {
	router.mu.RLock()
	defer router.mu.RUnlock()
	return router.config.Safe()
}

func (router *Router) Test(ctx context.Context, config Config) error {
	router.mu.RLock()
	current := router.config
	defaultPath := router.defaultPath
	router.mu.RUnlock()
	if config.Backend == BackendPostgreSQL && strings.TrimSpace(config.DSN) == "" && current.Backend == BackendPostgreSQL {
		config.DSN = current.DSN
	}
	normalized, err := config.Normalize(defaultPath)
	if err != nil {
		return err
	}
	repository, err := Open(ctx, normalized)
	if err != nil {
		return err
	}
	defer repository.Close()
	return repository.Ping(ctx)
}

func (router *Router) Configure(ctx context.Context, config Config) error {
	router.mu.RLock()
	current := router.config
	defaultPath := router.defaultPath
	router.mu.RUnlock()
	if config.Backend == BackendPostgreSQL && strings.TrimSpace(config.DSN) == "" && current.Backend == BackendPostgreSQL {
		config.DSN = current.DSN
	}
	normalized, err := config.Normalize(defaultPath)
	if err != nil {
		return err
	}
	replacement, err := Open(ctx, normalized)
	if err != nil {
		return err
	}
	if err := replacement.Ping(ctx); err != nil {
		replacement.Close()
		return err
	}
	if err := saveConfig(router.configPath, normalized); err != nil {
		replacement.Close()
		return err
	}
	router.mu.Lock()
	old := router.repository
	router.repository = replacement
	router.config = normalized
	router.mu.Unlock()
	return old.Close()
}

func (router *Router) Close() error {
	router.mu.Lock()
	repository := router.repository
	router.repository = nil
	router.mu.Unlock()
	if repository == nil {
		return nil
	}
	return repository.Close()
}

func (router *Router) withRepository(fn func(Repository, string) error) error {
	router.mu.RLock()
	defer router.mu.RUnlock()
	if router.repository == nil {
		return errors.New("session storage: repository is closed")
	}
	return fn(router.repository, router.projectID)
}

func Open(ctx context.Context, config Config) (Repository, error) {
	switch config.Backend {
	case BackendJSON:
		return newJSONRepository(config.Path)
	case BackendSQLite:
		return newSQLRepository(ctx, "sqlite", config.Path, "?")
	case BackendPostgreSQL:
		return newSQLRepository(ctx, "pgx", config.DSN, "$")
	default:
		return nil, fmt.Errorf("session storage: unsupported backend %q", config.Backend)
	}
}

type jsonManifest struct {
	Generation string                       `json:"generation"`
	Meta       frameworkStorage.SessionMeta `json:"meta"`
}

type jsonRepository struct {
	root string
	mu   sync.RWMutex
}

func newJSONRepository(root string) (*jsonRepository, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("session storage: create JSON root: %w", err)
	}
	return &jsonRepository{root: filepath.Clean(root)}, nil
}

func (repository *jsonRepository) WriteAtomic(_ context.Context, key Key, messages []types.Message) error {
	if err := key.validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	directory := repository.sessionDir(key)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	generation := "generation-" + randomID()
	generationDir := filepath.Join(directory, generation)
	if err := os.MkdirAll(generationDir, 0o700); err != nil {
		return err
	}
	shards := split(messages)
	for index, shard := range shards {
		data, err := json.Marshal(shard)
		if err != nil {
			return fmt.Errorf("session storage: marshal shard: %w", err)
		}
		if err := writeAtomic(filepath.Join(generationDir, fmt.Sprintf("history.%03d.json", index)), data, 0o600); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	meta := frameworkStorage.SessionMeta{SessionID: key.SessionID, TokenCount: tokenCount(messages), ShardCount: len(shards), UpdatedAt: now}
	if previous, err := repository.readManifest(directory); err == nil {
		meta.CreatedAt = previous.Meta.CreatedAt
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	data, err := json.Marshal(jsonManifest{Generation: generation, Meta: meta})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(directory, "manifest.json"), data, 0o600)
}

func (repository *jsonRepository) Read(_ context.Context, key Key) ([]types.Message, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	return repository.readAll(key)
}

func (repository *jsonRepository) ReadRange(ctx context.Context, key Key, offset, limit int) ([]types.Message, int, error) {
	if limit <= 0 || offset < 0 {
		return nil, 0, errors.New("session storage: invalid range")
	}
	messages, err := repository.Read(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	if offset > len(messages) {
		return nil, len(messages), errors.New("session storage: range offset exceeds history")
	}
	end := offset + limit
	if end > len(messages) {
		end = len(messages)
	}
	return append([]types.Message(nil), messages[offset:end]...), len(messages), nil
}

func (repository *jsonRepository) List(_ context.Context, projectID string) ([]frameworkStorage.SessionMeta, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	entries, err := os.ReadDir(repository.projectDir(projectID))
	if os.IsNotExist(err) {
		return []frameworkStorage.SessionMeta{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := make([]frameworkStorage.SessionMeta, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifest, err := repository.readManifest(filepath.Join(repository.projectDir(projectID), entry.Name()))
		if err == nil {
			result = append(result, manifest.Meta)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result, nil
}

func (repository *jsonRepository) Delete(_ context.Context, key Key) error {
	if err := key.validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if err := os.RemoveAll(repository.sessionDir(key)); err != nil {
		return err
	}
	return nil
}

func (repository *jsonRepository) Ping(context.Context) error { return nil }
func (repository *jsonRepository) Close() error               { return nil }

func (repository *jsonRepository) readAll(key Key) ([]types.Message, error) {
	directory := repository.sessionDir(key)
	manifest, err := repository.readManifest(directory)
	if err != nil {
		return nil, err
	}
	result := make([]types.Message, 0)
	for index := 0; index < manifest.Meta.ShardCount; index++ {
		data, err := os.ReadFile(filepath.Join(directory, manifest.Generation, fmt.Sprintf("history.%03d.json", index)))
		if err != nil {
			return nil, err
		}
		var shard []types.Message
		if err := json.Unmarshal(data, &shard); err != nil {
			return nil, err
		}
		result = append(result, shard...)
	}
	return result, nil
}

func (repository *jsonRepository) readManifest(directory string) (jsonManifest, error) {
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return jsonManifest{}, err
	}
	var manifest jsonManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return jsonManifest{}, err
	}
	return manifest, nil
}

func (repository *jsonRepository) projectDir(projectID string) string {
	return filepath.Join(repository.root, "project-"+hash(projectID))
}
func (repository *jsonRepository) sessionDir(key Key) string {
	return filepath.Join(repository.projectDir(key.ProjectID), "session-"+hash(key.SessionID))
}

type sqlRepository struct {
	db          *sql.DB
	placeholder string
}

func newSQLRepository(ctx context.Context, driver, dsn, placeholder string) (*sqlRepository, error) {
	if driver == "sqlite" {
		if err := os.MkdirAll(filepath.Dir(dsn), 0o700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	repository := &sqlRepository{db: db, placeholder: placeholder}
	if err := repository.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return repository, nil
}

func (repository *sqlRepository) Ping(ctx context.Context) error {
	if err := repository.db.PingContext(ctx); err != nil {
		return err
	}
	_, err := repository.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS seelex_sessions (
project_id TEXT NOT NULL, session_id TEXT NOT NULL, messages_json TEXT NOT NULL,
token_count INTEGER NOT NULL, shard_count INTEGER NOT NULL, created_at BIGINT NOT NULL,
updated_at BIGINT NOT NULL, PRIMARY KEY (project_id, session_id))`)
	return err
}

func (repository *sqlRepository) WriteAtomic(ctx context.Context, key Key, messages []types.Message) error {
	if err := key.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixNano()
	transaction, err := repository.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	args := []any{key.ProjectID, key.SessionID, string(data), tokenCount(messages), len(split(messages)), now, now}
	query := `INSERT INTO seelex_sessions (project_id,session_id,messages_json,token_count,shard_count,created_at,updated_at) VALUES (` + repository.placeholders(7) + `)
ON CONFLICT (project_id,session_id) DO UPDATE SET messages_json=excluded.messages_json, token_count=excluded.token_count, shard_count=excluded.shard_count, updated_at=excluded.updated_at`
	if _, err := transaction.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return transaction.Commit()
}

func (repository *sqlRepository) Read(ctx context.Context, key Key) ([]types.Message, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	query := `SELECT messages_json FROM seelex_sessions WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2)
	var data string
	if err := repository.db.QueryRowContext(ctx, query, key.ProjectID, key.SessionID).Scan(&data); err != nil {
		return nil, err
	}
	var messages []types.Message
	if err := json.Unmarshal([]byte(data), &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (repository *sqlRepository) ReadRange(ctx context.Context, key Key, offset, limit int) ([]types.Message, int, error) {
	if limit <= 0 || offset < 0 {
		return nil, 0, errors.New("session storage: invalid range")
	}
	messages, err := repository.Read(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	if offset > len(messages) {
		return nil, len(messages), errors.New("session storage: range offset exceeds history")
	}
	end := offset + limit
	if end > len(messages) {
		end = len(messages)
	}
	return append([]types.Message(nil), messages[offset:end]...), len(messages), nil
}

func (repository *sqlRepository) List(ctx context.Context, projectID string) ([]frameworkStorage.SessionMeta, error) {
	query := `SELECT session_id,token_count,shard_count,created_at,updated_at FROM seelex_sessions WHERE project_id=` + repository.arg(1) + ` ORDER BY updated_at DESC`
	rows, err := repository.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []frameworkStorage.SessionMeta{}
	for rows.Next() {
		var meta frameworkStorage.SessionMeta
		var createdAt, updatedAt int64
		if err := rows.Scan(&meta.SessionID, &meta.TokenCount, &meta.ShardCount, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		meta.CreatedAt, meta.UpdatedAt = time.Unix(0, createdAt).UTC(), time.Unix(0, updatedAt).UTC()
		result = append(result, meta)
	}
	return result, rows.Err()
}

func (repository *sqlRepository) Delete(ctx context.Context, key Key) error {
	if err := key.validate(); err != nil {
		return err
	}
	query := `DELETE FROM seelex_sessions WHERE project_id=` + repository.arg(1) + ` AND session_id=` + repository.arg(2)
	_, err := repository.db.ExecContext(ctx, query, key.ProjectID, key.SessionID)
	return err
}
func (repository *sqlRepository) Close() error { return repository.db.Close() }
func (repository *sqlRepository) arg(index int) string {
	if repository.placeholder == "$" {
		return fmt.Sprintf("$%d", index)
	}
	return "?"
}
func (repository *sqlRepository) placeholders(count int) string {
	values := make([]string, count)
	for i := range values {
		values[i] = repository.arg(i + 1)
	}
	return strings.Join(values, ",")
}

func split(messages []types.Message) [][]types.Message {
	if len(messages) == 0 {
		return [][]types.Message{{}}
	}
	result := make([][]types.Message, 0, (len(messages)+99)/100)
	for start := 0; start < len(messages); start += 100 {
		end := start + 100
		if end > len(messages) {
			end = len(messages)
		}
		result = append(result, messages[start:end])
	}
	return result
}
func tokenCount(messages []types.Message) int {
	total := 0
	for _, message := range messages {
		if message.Content != nil {
			total += (len(*message.Content) + 3) / 4
		}
	}
	return total
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
func randomID() string {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(data)
}
func writeAtomic(path string, data []byte, permission os.FileMode) error {
	temp := path + "." + randomID() + ".tmp"
	if err := os.WriteFile(temp, data, permission); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var config Config
	err = json.Unmarshal(data, &config)
	return config, err
}
func saveConfig(path string, config Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}
