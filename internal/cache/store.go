package cache

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/aneviaro/stratz-mcp/internal/config"
	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"
)

const (
	cacheFileName          = "cache.db"
	compressionThreshold   = 4 << 10
	defaultBusyTimeout     = 5 * time.Second
	defaultAsyncWriteLimit = 10 * time.Second
	defaultMaxPayloadBytes = 5 << 20
	maxPendingWrites       = 16
)

type Options struct {
	Config          config.CacheConfig
	Features        config.FeaturesConfig
	Logger          *slog.Logger
	Now             func() time.Time
	BusyTimeout     time.Duration
	MaxPayloadBytes int64
}

type Store struct {
	mu              sync.RWMutex
	db              *sql.DB
	path            string
	now             func() time.Time
	logger          *slog.Logger
	cacheConfig     config.CacheConfig
	features        config.FeaturesConfig
	status          Status
	reason          string
	busyTimeout     time.Duration
	maxPayloadBytes int64
	writeWG         sync.WaitGroup
	writeSlots      chan struct{}
	closeOnce       sync.Once
	closing         bool
}

func Open(options Options) (*Store, error) {
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	if !options.Config.Enabled {
		return &Store{
			logger:      logger,
			now:         now,
			cacheConfig: options.Config,
			features:    options.Features,
			status:      StatusDisabled,
			reason:      "cache is disabled by configuration",
		}, nil
	}
	directory := strings.TrimSpace(options.Config.Directory)
	if directory == "" {
		return nil, errors.New("cache directory is required")
	}
	if options.BusyTimeout <= 0 {
		options.BusyTimeout = defaultBusyTimeout
	}
	if options.MaxPayloadBytes <= 0 {
		options.MaxPayloadBytes = defaultMaxPayloadBytes
	}
	if err := ensureDirectory(directory); err != nil {
		return nil, err
	}
	path := filepath.Join(directory, cacheFileName)
	if err := ensureDatabaseFile(path); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open cache database: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	store := &Store{
		db:              database,
		path:            path,
		now:             now,
		logger:          logger,
		cacheConfig:     options.Config,
		features:        options.Features,
		status:          StatusHealthy,
		busyTimeout:     options.BusyTimeout,
		maxPayloadBytes: options.MaxPayloadBytes,
		writeSlots:      make(chan struct{}, maxPendingWrites),
	}
	if err := store.initialize(context.Background()); err != nil {
		_ = database.Close()
		return nil, err
	}
	return store, nil
}

func Degraded(logger *slog.Logger, reason string) *Store {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Store{
		logger: logger,
		now:    time.Now,
		status: StatusDegraded,
		reason: strings.TrimSpace(reason),
	}
}

func (store *Store) Status() Status {
	if store == nil {
		return StatusDisabled
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.status
}

func (store *Store) Reason() string {
	if store == nil {
		return "cache is unavailable"
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.reason
}

func (store *Store) Path() string {
	if store == nil {
		return ""
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.path
}

func (store *Store) Lookup(
	ctx context.Context,
	request LookupRequest,
) (LookupResult, error) {
	if store == nil {
		return LookupResult{Status: LookupDisabled}, nil
	}
	if status := store.Status(); status != StatusHealthy {
		return LookupResult{Status: LookupDisabled}, nil
	}
	if request.Fresh {
		return LookupResult{Status: LookupBypass}, nil
	}

	row := store.db.QueryRowContext(
		ctx,
		`SELECT payload, compression, logical_size_bytes, created_at_unix_ms, expires_at_unix_ms, stale_until_unix_ms
		FROM cache_entries
		WHERE namespace = ? AND domain = ? AND cache_key = ?`,
		request.Key.Namespace,
		request.Key.Domain,
		request.Key.Digest,
	)
	var (
		payload      []byte
		compression  string
		logicalSize  int64
		createdAtMS  int64
		expiresAtMS  int64
		staleUntilMS int64
	)
	if err := row.Scan(
		&payload,
		&compression,
		&logicalSize,
		&createdAtMS,
		&expiresAtMS,
		&staleUntilMS,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LookupResult{Status: LookupMiss}, nil
		}
		return LookupResult{Status: LookupDisabled}, store.fail(err, "read cache entry")
	}

	now := store.now().UTC()
	nowMS := now.UnixMilli()
	result := LookupResult{
		Status: LookupMiss,
		Age:    now.Sub(time.UnixMilli(createdAtMS)),
	}
	switch {
	case nowMS <= expiresAtMS:
		result.Status = LookupHit
	case request.AllowStale && nowMS <= staleUntilMS:
		result.Status = LookupStale
	default:
		return result, nil
	}
	decoded, err := decodePayload(payload, compression, logicalSize, store.maxPayloadBytes)
	if err != nil {
		return LookupResult{Status: LookupDisabled}, store.fail(err, "decode cache entry")
	}
	if _, err := store.db.ExecContext(
		ctx,
		`UPDATE cache_entries
		SET accessed_at_unix_ms = ?
		WHERE namespace = ? AND domain = ? AND cache_key = ?`,
		nowMS,
		request.Key.Namespace,
		request.Key.Domain,
		request.Key.Digest,
	); err != nil {
		return LookupResult{Status: LookupDisabled}, store.fail(err, "update cache access time")
	}
	result.Payload = decoded
	return result, nil
}

func (store *Store) PutAsync(entry Entry) {
	if store == nil {
		return
	}
	store.mu.Lock()
	if store.closing || store.status != StatusHealthy || store.db == nil {
		store.mu.Unlock()
		return
	}
	select {
	case store.writeSlots <- struct{}{}:
	default:
		store.mu.Unlock()
		store.logger.Warn("cache write dropped because the pending-write limit was reached")
		return
	}
	store.writeWG.Add(1)
	store.mu.Unlock()
	go func() {
		defer store.writeWG.Done()
		defer func() { <-store.writeSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), defaultAsyncWriteLimit)
		defer cancel()
		err := store.Put(ctx, entry)
		if err != nil && !errors.Is(err, ErrNotCacheable) &&
			!errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			store.logger.Warn("cache write failed", "error", err)
		}
	}()
}

func (store *Store) Put(ctx context.Context, entry Entry) error {
	if store == nil {
		return nil
	}
	if status := store.Status(); status != StatusHealthy {
		return nil
	}
	if !entry.Classification.Cacheable {
		return ErrNotCacheable
	}
	if entry.Key.Namespace == "" || entry.Key.Domain == "" || entry.Key.Digest == "" {
		return errors.New("cache key is incomplete")
	}

	payload, compression, err := encodePayload(entry.Payload)
	if err != nil {
		return err
	}
	now := store.now().UTC()
	createdAtMS := now.UnixMilli()
	expiresAtMS := now.Add(entry.Classification.TTL).UnixMilli()
	staleUntilMS := now.Add(entry.Classification.TTL + entry.Classification.Stale).UnixMilli()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return store.fail(err, "begin cache write")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO cache_entries (
			namespace,
			domain,
			class,
			cache_key,
			format_version,
			payload,
			compression,
			logical_size_bytes,
			stored_size_bytes,
			created_at_unix_ms,
			accessed_at_unix_ms,
			expires_at_unix_ms,
			stale_until_unix_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(namespace, domain, cache_key) DO UPDATE SET
			class = excluded.class,
			format_version = excluded.format_version,
			payload = excluded.payload,
			compression = excluded.compression,
			logical_size_bytes = excluded.logical_size_bytes,
			stored_size_bytes = excluded.stored_size_bytes,
			created_at_unix_ms = excluded.created_at_unix_ms,
			accessed_at_unix_ms = excluded.accessed_at_unix_ms,
			expires_at_unix_ms = excluded.expires_at_unix_ms,
			stale_until_unix_ms = excluded.stale_until_unix_ms`,
		entry.Key.Namespace,
		entry.Key.Domain,
		string(entry.Key.Class),
		entry.Key.Digest,
		FormatVersion,
		payload,
		compression,
		len(entry.Payload),
		len(payload),
		createdAtMS,
		createdAtMS,
		expiresAtMS,
		staleUntilMS,
	); err != nil {
		return store.fail(err, "upsert cache entry")
	}
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM cache_entries WHERE stale_until_unix_ms < ?`,
		now.UnixMilli(),
	); err != nil {
		return store.fail(err, "purge expired cache entries")
	}
	if err := evictLRU(ctx, tx, store.cacheConfig.MaxSizeBytes); err != nil {
		return store.fail(err, "evict cache entries")
	}
	if err := tx.Commit(); err != nil {
		return store.fail(err, "commit cache write")
	}
	committed = true
	return ensureSidecarPermissions(store.path)
}

func (store *Store) Stats(ctx context.Context) (Stats, error) {
	if store == nil {
		return Stats{Status: StatusDisabled}, nil
	}
	status := store.Status()
	if status != StatusHealthy {
		return Stats{Status: status, Reason: store.Reason(), FormatVersion: FormatVersion}, nil
	}
	stats := Stats{
		Status:        StatusHealthy,
		FormatVersion: FormatVersion,
	}
	row := store.db.QueryRowContext(
		ctx,
		`SELECT
			COUNT(*),
			COUNT(DISTINCT namespace),
			COALESCE(SUM(logical_size_bytes), 0),
			COALESCE(SUM(stored_size_bytes), 0)
		FROM cache_entries`,
	)
	if err := row.Scan(
		&stats.Entries,
		&stats.Namespaces,
		&stats.LogicalBytes,
		&stats.StoredBytes,
	); err != nil {
		return Stats{Status: StatusDegraded}, store.fail(err, "query cache statistics")
	}

	rows, err := store.db.QueryContext(
		ctx,
		`SELECT domain, COUNT(*), COALESCE(SUM(logical_size_bytes), 0), COALESCE(SUM(stored_size_bytes), 0)
		FROM cache_entries
		GROUP BY domain
		ORDER BY domain`,
	)
	if err != nil {
		return Stats{Status: StatusDegraded}, store.fail(err, "query cache domain statistics")
	}
	defer rows.Close()
	for rows.Next() {
		var domain DomainStats
		if err := rows.Scan(
			&domain.Domain,
			&domain.Entries,
			&domain.LogicalBytes,
			&domain.StoredBytes,
		); err != nil {
			return Stats{Status: StatusDegraded}, store.fail(err, "scan cache domain statistics")
		}
		stats.Domains = append(stats.Domains, domain)
	}
	if err := rows.Err(); err != nil {
		return Stats{Status: StatusDegraded}, store.fail(err, "iterate cache domain statistics")
	}
	return stats, nil
}

func (store *Store) Clear(ctx context.Context, options ClearOptions) (ClearResult, error) {
	if store == nil {
		return ClearResult{}, nil
	}
	if status := store.Status(); status != StatusHealthy {
		return ClearResult{}, fmt.Errorf("cache is %s: %s", status, store.Reason())
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return ClearResult{}, store.fail(err, "begin cache clear")
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	query := `DELETE FROM cache_entries`
	var arguments []any
	var clauses []string
	if domain := domainName(options.Domain); domain != "" {
		clauses = append(clauses, "domain = ?")
		arguments = append(arguments, domain)
	}
	if namespace := strings.TrimSpace(options.Namespace); namespace != "" {
		clauses = append(clauses, "namespace = ?")
		arguments = append(arguments, namespace)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	result, err := tx.ExecContext(ctx, query, arguments...)
	if err != nil {
		return ClearResult{}, store.fail(err, "clear cache entries")
	}
	if err := tx.Commit(); err != nil {
		return ClearResult{}, store.fail(err, "commit cache clear")
	}
	committed = true
	deleted, _ := result.RowsAffected()
	return ClearResult{Deleted: deleted}, nil
}

func (store *Store) Close() error {
	if store == nil {
		return nil
	}
	var err error
	store.closeOnce.Do(func() {
		store.mu.Lock()
		store.closing = true
		store.mu.Unlock()
		store.writeWG.Wait()
		store.mu.Lock()
		if store.status == StatusHealthy {
			store.status = StatusDisabled
			store.reason = "cache is closed"
		}
		store.mu.Unlock()
		if store.db != nil {
			err = store.db.Close()
		}
	})
	return err
}

func (store *Store) initialize(ctx context.Context) error {
	if _, err := store.db.ExecContext(
		ctx,
		fmt.Sprintf("PRAGMA busy_timeout = %d", store.busyTimeout.Milliseconds()),
	); err != nil {
		return fmt.Errorf("configure cache busy timeout: %w", err)
	}
	var journalMode string
	if err := store.db.QueryRowContext(
		ctx,
		"PRAGMA journal_mode = WAL",
	).Scan(&journalMode); err != nil {
		return fmt.Errorf("configure cache WAL mode: %w", err)
	}
	if _, err := store.db.ExecContext(ctx, "PRAGMA synchronous = NORMAL"); err != nil {
		return fmt.Errorf("configure cache synchronous mode: %w", err)
	}
	if err := migrate(ctx, store.db); err != nil {
		return err
	}
	return ensureSidecarPermissions(store.path)
}

func migrate(ctx context.Context, database *sql.DB) error {
	var version int
	if err := database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read cache format version: %w", err)
	}
	if version > FormatVersion {
		return fmt.Errorf(
			"cache format version %d is newer than supported version %d",
			version,
			FormatVersion,
		)
	}
	if version == FormatVersion {
		return nil
	}
	if version != 0 {
		return fmt.Errorf("unsupported cache format version %d", version)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cache migration: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	statements := []string{
		`CREATE TABLE cache_entries (
			namespace TEXT NOT NULL,
			domain TEXT NOT NULL,
			class TEXT NOT NULL,
			cache_key TEXT NOT NULL,
			format_version INTEGER NOT NULL,
			payload BLOB NOT NULL,
			compression TEXT NOT NULL,
			logical_size_bytes INTEGER NOT NULL,
			stored_size_bytes INTEGER NOT NULL,
			created_at_unix_ms INTEGER NOT NULL,
			accessed_at_unix_ms INTEGER NOT NULL,
			expires_at_unix_ms INTEGER NOT NULL,
			stale_until_unix_ms INTEGER NOT NULL,
			PRIMARY KEY(namespace, domain, cache_key)
		)`,
		`CREATE INDEX cache_entries_domain_idx ON cache_entries(domain, namespace)`,
		`CREATE INDEX cache_entries_accessed_idx ON cache_entries(accessed_at_unix_ms, namespace, domain, cache_key)`,
		`CREATE INDEX cache_entries_expiry_idx ON cache_entries(stale_until_unix_ms)`,
		fmt.Sprintf(`PRAGMA user_version = %d`, FormatVersion),
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply cache migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cache migration: %w", err)
	}
	committed = true
	return nil
}

func evictLRU(ctx context.Context, tx *sql.Tx, maxSizeBytes int64) error {
	var totalBytes int64
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COALESCE(SUM(stored_size_bytes), 0) FROM cache_entries`,
	).Scan(&totalBytes); err != nil {
		return err
	}
	if totalBytes <= maxSizeBytes {
		return nil
	}

	rows, err := tx.QueryContext(
		ctx,
		`SELECT namespace, domain, cache_key, stored_size_bytes
		FROM cache_entries
		ORDER BY accessed_at_unix_ms ASC, created_at_unix_ms ASC, namespace ASC, domain ASC, cache_key ASC`,
	)
	if err != nil {
		return err
	}
	defer rows.Close()
	type victim struct {
		namespace  string
		domain     string
		digest     string
		storedSize int64
	}
	var victims []victim
	for rows.Next() {
		var candidate victim
		if err := rows.Scan(
			&candidate.namespace,
			&candidate.domain,
			&candidate.digest,
			&candidate.storedSize,
		); err != nil {
			return err
		}
		victims = append(victims, candidate)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, candidate := range victims {
		if totalBytes <= maxSizeBytes {
			break
		}
		if _, err := tx.ExecContext(
			ctx,
			`DELETE FROM cache_entries
			WHERE namespace = ? AND domain = ? AND cache_key = ?`,
			candidate.namespace,
			candidate.domain,
			candidate.digest,
		); err != nil {
			return err
		}
		totalBytes -= candidate.storedSize
	}
	return nil
}

func encodePayload(payload []byte) ([]byte, string, error) {
	if len(payload) <= compressionThreshold {
		return append([]byte(nil), payload...), "none", nil
	}
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, "", err
	}
	defer encoder.Close()
	return encoder.EncodeAll(payload, nil), "zstd", nil
}

func decodePayload(payload []byte, compression string, logicalSize, maximum int64) ([]byte, error) {
	if logicalSize < 0 || logicalSize > maximum {
		return nil, fmt.Errorf("cache logical payload size %d exceeds limit %d", logicalSize, maximum)
	}
	switch compression {
	case "", "none":
		if int64(len(payload)) != logicalSize {
			return nil, fmt.Errorf("cache logical payload size mismatch")
		}
		return append([]byte(nil), payload...), nil
	case "zstd":
		decoder, err := zstd.NewReader(
			bytes.NewReader(payload),
			zstd.WithDecoderMaxMemory(uint64(maximum)),
			zstd.WithDecoderMaxWindow(uint64(maximum)),
		)
		if err != nil {
			return nil, err
		}
		defer decoder.Close()
		decoded, err := io.ReadAll(io.LimitReader(decoder, maximum+1))
		if err != nil {
			return nil, err
		}
		if int64(len(decoded)) != logicalSize {
			return nil, fmt.Errorf("cache logical payload size mismatch")
		}
		return decoded, nil
	default:
		return nil, fmt.Errorf("unknown cache compression %q", compression)
	}
}

func ensureDirectory(directory string) error {
	info, err := os.Lstat(directory)
	switch {
	case os.IsNotExist(err):
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create cache directory: %w", err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(directory, 0o700); err != nil {
				return fmt.Errorf("chmod cache directory: %w", err)
			}
		}
		return nil
	case err != nil:
		return fmt.Errorf("inspect cache directory: %w", err)
	case info.Mode()&os.ModeSymlink != 0:
		return errors.New("cache directory must not be a symlink")
	case !info.IsDir():
		return errors.New("cache directory path is not a directory")
	default:
		if runtime.GOOS != "windows" {
			if err := os.Chmod(directory, 0o700); err != nil {
				return fmt.Errorf("chmod cache directory: %w", err)
			}
		}
		return nil
	}
}

func ensureDatabaseFile(path string) error {
	info, err := os.Lstat(path)
	switch {
	case os.IsNotExist(err):
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			return fmt.Errorf("create cache database: %w", err)
		}
		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("close created cache database: %w", closeErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("inspect cache database: %w", err)
	case info.Mode()&os.ModeSymlink != 0:
		return errors.New("cache database must not be a symlink")
	case !info.Mode().IsRegular():
		return errors.New("cache database path is not a regular file")
	default:
		if runtime.GOOS != "windows" {
			if err := os.Chmod(path, 0o600); err != nil {
				return fmt.Errorf("chmod cache database: %w", err)
			}
		}
		return nil
	}
}

func ensureSidecarPermissions(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		candidate := path + suffix
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect cache sidecar: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("cache database sidecar must not be a symlink")
		}
		if !info.Mode().IsRegular() {
			return errors.New("cache database sidecar is not a regular file")
		}
		if err := os.Chmod(candidate, 0o600); err != nil {
			return fmt.Errorf("chmod cache sidecar: %w", err)
		}
	}
	return nil
}

func (store *Store) fail(err error, action string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.status == StatusHealthy {
		store.status = StatusDegraded
		store.reason = fmt.Sprintf("%s failed: %v", action, err)
		store.logger.Warn("disabling cache for this process", "error", err, "action", action)
	}
	return err
}
