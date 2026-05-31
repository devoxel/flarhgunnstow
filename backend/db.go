package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

// schema is the database schema applied at NewStore time. It is idempotent
// (CREATE TABLE IF NOT EXISTS) so it doubles as a migration for the initial
// version. When the schema evolves, a `user_version` based migration scheme
// can be layered on top.
const schema = `
CREATE TABLE IF NOT EXISTS guilds (
    guild_id   TEXT PRIMARY KEY,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS playlists (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    guild_id   TEXT NOT NULL REFERENCES guilds(guild_id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    category   TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    uploader   TEXT NOT NULL DEFAULT 'anon',
    UNIQUE (guild_id, title)
);

CREATE INDEX IF NOT EXISTS idx_playlists_guild ON playlists(guild_id);

CREATE TABLE IF NOT EXISTS tracks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    uploader    TEXT NOT NULL DEFAULT 'anon',
    url         TEXT NOT NULL DEFAULT '',
    path        TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_tracks_playlist ON tracks(playlist_id);

CREATE TABLE IF NOT EXISTS default_playlists (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    title    TEXT NOT NULL UNIQUE,
    category TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS default_tracks (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    default_pl_id INTEGER NOT NULL REFERENCES default_playlists(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    url           TEXT NOT NULL DEFAULT '',
    path          TEXT NOT NULL DEFAULT ''
);
`

var (
	ErrPlaylistNotFound = errors.New("playlist not found")
)

// builtinDefaults is the minimal default playlist set shipped with the bot.
// TODO: replace with actual SQL directly.
var builtinDefaults = []Playlist{
	{
		Title:    "Bangers",
		Category: "example",
		Tracks: []Track{
			{
				Name: "Aquatic Ambiance",
				Path: "sample.opus",
			},
		},
	},
}

// Store wraps a SQLite database. All exported methods are safe for concurrent
// use; they serialize on the database connection (MaxOpenConns=1) which keeps
// SQLite happy without us having to reason about per-method locking.
type Store struct {
	db *sql.DB

	// seedOnce guards the one-time default playlist seed. Cloning to a guild
	// is a separate, idempotent step.
	seedOnce sync.Once
	seedErr  error
}

// NewStore opens (or creates) the SQLite database at path, applies the schema,
// and seeds default playlists. WAL mode is enabled for concurrent reader behaviour.
//
// Pass ":memory:" or "file::memory:?cache=shared" for an in-memory DB (tests).
func NewStore(path string) (*Store, error) {
	dsn := path
	if !strings.HasPrefix(dsn, "file:") && !strings.Contains(dsn, ":memory:") {
		// Enable foreign keys via DSN so they apply on every connection.
		dsn = "file:" + dsn + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite handles concurrency well with WAL, but cap connections to keep
	// reasoning simple. One open / one idle is plenty for this workload.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	s := &Store{db: db}

	if err := s.seedDefaults(); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed defaults: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// seedDefaults populates the default_* tables once per Store lifetime.
// It is idempotent at the row level (INSERT OR IGNORE) so calling it against a populated DB is a no-op.
func (s *Store) seedDefaults() error {
	s.seedOnce.Do(func() {
		s.seedErr = s.doSeedDefaults()
	})
	return s.seedErr
}

func (s *Store) doSeedDefaults() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, pl := range builtinDefaults {
		if pl.Title == "" || pl.Category == "" {
			continue
		}
		res, err := tx.Exec(
			`INSERT OR IGNORE INTO default_playlists(title, category) VALUES (?, ?)`,
			pl.Title, pl.Category,
		)
		if err != nil {
			return fmt.Errorf("insert default playlist %q: %w", pl.Title, err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			continue // already seeded from a prior run
		}
		var plID int64
		if err := tx.QueryRow(
			`SELECT id FROM default_playlists WHERE title = ?`, pl.Title,
		).Scan(&plID); err != nil {
			return fmt.Errorf("lookup default playlist %q: %w", pl.Title, err)
		}
		for _, t := range pl.Tracks {
			if _, err := tx.Exec(
				`INSERT INTO default_tracks(default_pl_id, name, url, path) VALUES (?, ?, ?, ?)`,
				plID, t.Name, t.URL, t.Path,
			); err != nil {
				return fmt.Errorf("insert default track %q: %w", t.Name, err)
			}
		}
	}

	return tx.Commit()
}

// AddLocalDefaults registers any local audio files from videoDir as default
// playlists, so they get cloned into each new guild alongside the shipped
// sample playlists. Mirrors the old initFromDir behaviour but writes to SQL.
//
// Files at the root of videoDir become a single playlist named after the
// directory. Each subdirectory becomes its own playlist.
//
// Safe to call repeatedly: existing default_playlists rows are left alone.
func (s *Store) AddLocalDefaults(videoDir string) error {
	if videoDir == "" || videoDir == "." {
		return nil
	}

	entries, err := os.ReadDir(videoDir)
	if err != nil {
		return fmt.Errorf("read video dir: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	addPlaylist := func(title, category string, tracks []Track) error {
		res, err := tx.Exec(
			`INSERT OR IGNORE INTO default_playlists(title, category) VALUES (?, ?)`,
			title, category,
		)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil // already present, leave alone
		}
		var plID int64
		if err := tx.QueryRow(
			`SELECT id FROM default_playlists WHERE title = ?`, title,
		).Scan(&plID); err != nil {
			return err
		}
		for _, t := range tracks {
			if _, err := tx.Exec(
				`INSERT INTO default_tracks(default_pl_id, name, url, path) VALUES (?, ?, ?, ?)`,
				plID, t.Name, t.URL, t.Path,
			); err != nil {
				return err
			}
		}
		log.Printf("AddLocalDefaults: added playlist %q (%d tracks)", title, len(tracks))
		return nil
	}

	var rootTracks []Track
	for _, e := range entries {
		if e.IsDir() {
			subDir := filepath.Join(videoDir, e.Name())
			subEntries, err := os.ReadDir(subDir)
			if err != nil {
				log.Printf("AddLocalDefaults: cannot read subdir %s: %v", subDir, err)
				continue
			}
			var tracks []Track
			for _, f := range subEntries {
				if f.IsDir() {
					continue
				}
				if !audioExts[strings.ToLower(filepath.Ext(f.Name()))] {
					continue
				}
				name := strings.TrimSuffix(f.Name(), filepath.Ext(f.Name()))
				tracks = append(tracks, Track{
					Name: name,
					Path: filepath.Join(subDir, f.Name()),
				})
			}
			if len(tracks) == 0 {
				continue
			}
			if err := addPlaylist(e.Name(), "Local", tracks); err != nil {
				return err
			}
		} else {
			if !audioExts[strings.ToLower(filepath.Ext(e.Name()))] {
				continue
			}
			name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			rootTracks = append(rootTracks, Track{
				Name: name,
				Path: filepath.Join(videoDir, e.Name()),
			})
		}
	}

	if len(rootTracks) > 0 {
		title := filepath.Base(videoDir)
		if title == "." || title == "/" || title == "" {
			title = "Local Files"
		}
		if err := addPlaylist(title, "Local", rootTracks); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// EnsureGuild upserts a guild row, refreshing updated_at. Idempotent.
func (s *Store) EnsureGuild(guildID string) error {
	_, err := s.db.Exec(`
		INSERT INTO guilds(guild_id) VALUES (?)
		ON CONFLICT(guild_id) DO UPDATE SET updated_at = datetime('now')
	`, guildID)
	if err != nil {
		return fmt.Errorf("EnsureGuild: %w", err)
	}
	return nil
}

// CloneDefaultPlaylists copies every default playlist (and its tracks) into
// the given guild, skipping titles the guild already owns. Runs in a single
// transaction so a partial failure leaves the guild in its previous state.
func (s *Store) CloneDefaultPlaylists(guildID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT id, title, category FROM default_playlists ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list defaults: %w", err)
	}
	type defPL struct {
		ID       int64
		Title    string
		Category string
	}
	var defs []defPL
	for rows.Next() {
		var d defPL
		if err := rows.Scan(&d.ID, &d.Title, &d.Category); err != nil {
			rows.Close()
			return err
		}
		defs = append(defs, d)
	}
	rows.Close()

	for _, d := range defs {
		res, err := tx.Exec(
			`INSERT OR IGNORE INTO playlists(guild_id, title, category) VALUES (?, ?, ?)`,
			guildID, d.Title, d.Category,
		)
		if err != nil {
			return fmt.Errorf("clone playlist %q: %w", d.Title, err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			continue // guild already has a playlist with this title
		}
		var newID int64
		if err := tx.QueryRow(
			`SELECT id FROM playlists WHERE guild_id = ? AND title = ?`,
			guildID, d.Title,
		).Scan(&newID); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO tracks(playlist_id, name, url, path)
			SELECT ?, name, url, path FROM default_tracks
			WHERE default_pl_id = ? ORDER BY id
		`, newID, d.ID); err != nil {
			return fmt.Errorf("clone tracks for %q: %w", d.Title, err)
		}
	}

	return tx.Commit()
}

// LoadPlaylists returns every playlist for a guild, ordered by title, with
// each playlist's tracks ordered by insertion. Tracks are loaded eagerly —
// the dataset is small (hundreds of tracks) and the API caller serializes
// the result to JSON immediately.
func (s *Store) LoadPlaylists(guildID string) ([]*Playlist, error) {
	rows, err := s.db.Query(`
		SELECT id, title, category FROM playlists
		WHERE guild_id = ? ORDER BY title
	`, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type plRow struct {
		ID       int64
		Title    string
		Category string
	}
	var pls []plRow
	for rows.Next() {
		var r plRow
		if err := rows.Scan(&r.ID, &r.Title, &r.Category); err != nil {
			return nil, err
		}
		pls = append(pls, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]*Playlist, 0, len(pls))
	for _, r := range pls {
		tracks, err := s.loadTracksForPlaylist(r.ID)
		if err != nil {
			return nil, fmt.Errorf("load tracks for %q: %w", r.Title, err)
		}
		out = append(out, &Playlist{
			Title:    r.Title,
			Category: r.Category,
			Tracks:   tracks,
		})
	}
	return out, nil
}

// LoadPlaylistByTitle fetches a single playlist by title for a guild.
func (s *Store) LoadPlaylistByTitle(guildID, title string) (*Playlist, error) {
	var id int64
	var category string
	err := s.db.QueryRow(
		`SELECT id, category FROM playlists WHERE guild_id = ? AND title = ?`,
		guildID, title,
	).Scan(&id, &category)
	if err == sql.ErrNoRows {
		return nil, ErrPlaylistNotFound
	} else if err != nil {
		return nil, err
	}
	tracks, err := s.loadTracksForPlaylist(id)
	if err != nil {
		return nil, err
	}
	return &Playlist{Title: title, Category: category, Tracks: tracks}, nil
}

func (s *Store) loadTracksForPlaylist(plID int64) ([]Track, error) {
	rows, err := s.db.Query(
		`SELECT name, uploader, url, path FROM tracks WHERE playlist_id = ? ORDER BY id`, plID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []Track
	for rows.Next() {
		var t Track
		if err := rows.Scan(&t.Name, &t.Uploader, &t.URL, &t.Path); err != nil {
			return nil, err
		}
		tracks = append(tracks, t)
	}
	return tracks, rows.Err()
}

// SavePlaylist writes a playlist for a guild. If a playlist with the same
// title already exists, it is fully replaced (tracks and all) inside a single
// transaction so concurrent readers either see the old or new version.
func (s *Store) SavePlaylist(guildID string, p *Playlist) error {
	if p == nil {
		return errors.New("nil playlist")
	}
	if p.Title == "" {
		return errors.New("empty title")
	}
	if p.Category == "" {
		return errors.New("empty category")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Best-effort delete of any existing playlist with this title; ON DELETE
	// CASCADE removes its tracks.
	if _, err := tx.Exec(
		`DELETE FROM playlists WHERE guild_id = ? AND title = ?`,
		guildID, p.Title,
	); err != nil {
		return fmt.Errorf("clear existing: %w", err)
	}

	res, err := tx.Exec(
		`INSERT INTO playlists(guild_id, title, category) VALUES (?, ?, ?)`,
		guildID, p.Title, p.Category,
	)
	if err != nil {
		return fmt.Errorf("insert playlist: %w", err)
	}
	plID, err := res.LastInsertId()
	if err != nil {
		return err
	}

	for _, t := range p.Tracks {
		if _, err := tx.Exec(
			`INSERT INTO tracks(playlist_id, name, uploader, url, path) VALUES (?, ?, ?, ?, ?)`,
			plID, t.Name, t.Uploader, t.URL, t.Path,
		); err != nil {
			return fmt.Errorf("insert track: %w", err)
		}
	}

	return tx.Commit()
}

// DeletePlaylist removes a playlist (and its tracks via cascade) for a guild.
// Returns ErrPlaylistNotFound if no playlist matches the title.
func (s *Store) DeletePlaylist(guildID, title string) error {
	res, err := s.db.Exec(
		`DELETE FROM playlists WHERE guild_id = ? AND title = ?`,
		guildID, title,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrPlaylistNotFound
	}
	return nil
}
