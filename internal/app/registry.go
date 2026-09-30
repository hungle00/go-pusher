package app

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type App struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Key    string `json:"key"`
	Secret string `json:"secret"`
}

type Registry struct {
	db *sql.DB
}

func OpenRegistry(path string) (*Registry, error) {
	if path == "" {
		return nil, errors.New("sqlite database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS apps (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			public_key TEXT NOT NULL UNIQUE,
			secret TEXT NOT NULL,
			secret_hash BLOB NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureSchemaColumns(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Registry{db: db}, nil
}

func ensureSchemaColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(apps)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		columns[name] = true
	}
	if !columns["secret"] {
		if _, err := db.Exec(`ALTER TABLE apps ADD COLUMN secret TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	return nil
}

func (r *Registry) Close() error {
	return r.db.Close()
}

func (r *Registry) Create(name string) (App, error) {
	return r.createWithSecret(name, randomString("secret_"))
}

func (r *Registry) FindOrCreateApp(name string) (App, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return App{}, errors.New("app name is required")
	}

	var appID, appName, appKey, secret string
	if err := r.db.QueryRow(`SELECT id, name, public_key, secret FROM apps WHERE name = ? LIMIT 1`, trimmed).Scan(&appID, &appName, &appKey, &secret); err == nil {
		return App{ID: appID, Name: appName, Key: appKey, Secret: secret}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return App{}, err
	}

	return r.createWithSecret(trimmed, randomString("secret_"))
}

func (r *Registry) createWithSecret(name, secret string) (App, error) {
	id, err := randomToken("app_")
	if err != nil {
		return App{}, err
	}
	key, err := randomToken("key_")
	if err != nil {
		return App{}, err
	}

	created := App{ID: id, Name: strings.TrimSpace(name), Key: key, Secret: secret}
	secretHash := sha256.Sum256([]byte(secret))
	_, err = r.db.Exec(
		`INSERT INTO apps (id, name, public_key, secret, secret_hash) VALUES (?, ?, ?, ?, ?)`,
		created.ID, created.Name, created.Key, created.Secret, secretHash[:],
	)
	if err != nil {
		return App{}, err
	}
	return created, nil
}

func (r *Registry) HasKey(id, key string) (bool, error) {
	var storedKey string
	err := r.db.QueryRow(`SELECT public_key FROM apps WHERE id = ?`, id).Scan(&storedKey)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(storedKey), []byte(key)) == 1, nil
}

func (r *Registry) HasSecret(id, secret string) (bool, error) {
	var storedHash []byte
	err := r.db.QueryRow(`SELECT secret_hash FROM apps WHERE id = ?`, id).Scan(&storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	secretHash := sha256.Sum256([]byte(secret))
	return subtle.ConstantTimeCompare(storedHash, secretHash[:]) == 1, nil
}

func randomString(prefix string) string {
	value, err := randomToken(prefix)
	if err != nil {
		panic(err)
	}
	return value
}

func randomToken(prefix string) (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(value), nil
}
