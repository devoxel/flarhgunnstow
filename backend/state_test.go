package main

import (
	"reflect"
	"testing"
)

// newTestStore opens an isolated in-memory Store for one test. Each call gets
// its own database; nothing leaks between tests.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	// A fresh URI per test ensures the in-memory DB is not shared.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=foreign_keys(1)"
	s, err := NewStore(dsn)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_GuildIdempotent(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 3; i++ {
		if err := s.EnsureGuild("g1"); err != nil {
			t.Fatalf("EnsureGuild %d: %v", i, err)
		}
	}

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM guilds WHERE guild_id = ?`, "g1").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 guild row, got %d", n)
	}
}

func TestStore_CloneDefaultsOnce(t *testing.T) {
	s := newTestStore(t)
	if err := s.EnsureGuild("g1"); err != nil {
		t.Fatal(err)
	}
	if err := s.CloneDefaultPlaylists("g1"); err != nil {
		t.Fatal(err)
	}
	first, err := s.LoadPlaylists("g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(builtinDefaults) {
		t.Fatalf("want %d default playlists cloned, got %d", len(builtinDefaults), len(first))
	}

	// Second clone should be a no-op.
	if err := s.CloneDefaultPlaylists("g1"); err != nil {
		t.Fatal(err)
	}
	second, err := s.LoadPlaylists("g1")
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != len(first) {
		t.Fatalf("clone re-ran: had %d playlists, now %d", len(first), len(second))
	}
}

func TestStore_SavePlaylistAndLoad(t *testing.T) {
	s := newTestStore(t)
	if err := s.EnsureGuild("g1"); err != nil {
		t.Fatal(err)
	}
	pl := &Playlist{
		Title:    "Test",
		Category: "Misc",
		Tracks: []Track{
			{Name: "a", URL: "u-a"},
			{Name: "b", Path: "/p/b"},
		},
	}
	if err := s.SavePlaylist("g1", pl); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadPlaylistByTitle("g1", "Test")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Tracks, pl.Tracks) {
		t.Fatalf("tracks mismatch:\nwant %#v\ngot  %#v", pl.Tracks, got.Tracks)
	}
}

func TestStore_SavePlaylistReplaces(t *testing.T) {
	s := newTestStore(t)
	if err := s.EnsureGuild("g1"); err != nil {
		t.Fatal(err)
	}
	pl := &Playlist{Title: "T", Category: "C", Tracks: []Track{{Name: "x"}}}
	if err := s.SavePlaylist("g1", pl); err != nil {
		t.Fatal(err)
	}
	pl2 := &Playlist{Title: "T", Category: "C", Tracks: []Track{{Name: "y"}, {Name: "z"}}}
	if err := s.SavePlaylist("g1", pl2); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadPlaylistByTitle("g1", "T")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tracks) != 2 || got.Tracks[0].Name != "y" || got.Tracks[1].Name != "z" {
		t.Fatalf("expected replacement tracks, got %#v", got.Tracks)
	}
}

func TestStore_DeletePlaylistCascades(t *testing.T) {
	s := newTestStore(t)
	if err := s.EnsureGuild("g1"); err != nil {
		t.Fatal(err)
	}
	pl := &Playlist{Title: "T", Category: "C", Tracks: []Track{{Name: "x"}, {Name: "y"}}}
	if err := s.SavePlaylist("g1", pl); err != nil {
		t.Fatal(err)
	}

	if err := s.DeletePlaylist("g1", "T"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadPlaylistByTitle("g1", "T"); err != ErrPlaylistNotFound {
		t.Fatalf("want ErrPlaylistNotFound, got %v", err)
	}

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("tracks not cascaded: %d remain", n)
	}

	if err := s.DeletePlaylist("g1", "T"); err != ErrPlaylistNotFound {
		t.Fatalf("deleting missing should return ErrPlaylistNotFound, got %v", err)
	}
}

func TestStore_GuildIsolation(t *testing.T) {
	s := newTestStore(t)
	for _, g := range []string{"g1", "g2"} {
		if err := s.EnsureGuild(g); err != nil {
			t.Fatal(err)
		}
	}
	pl := &Playlist{Title: "Mine", Category: "Misc", Tracks: []Track{{Name: "a"}}}
	if err := s.SavePlaylist("g1", pl); err != nil {
		t.Fatal(err)
	}

	g1, err := s.LoadPlaylists("g1")
	if err != nil {
		t.Fatal(err)
	}
	g2, err := s.LoadPlaylists("g2")
	if err != nil {
		t.Fatal(err)
	}
	foundG1 := false
	for _, p := range g1 {
		if p.Title == "Mine" {
			foundG1 = true
		}
	}
	if !foundG1 {
		t.Fatal("expected 'Mine' visible to g1")
	}
	for _, p := range g2 {
		if p.Title == "Mine" {
			t.Fatal("g2 should not see g1's playlist")
		}
	}
}

func TestSessionManager_FromOrCreate(t *testing.T) {
	s := newTestStore(t)
	mgr := NewSessionManager(s)

	noopMsg := func(string) error { return nil }
	sess1, sid1, err := mgr.FromOrCreate("guild-A", noopMsg, nil)
	if err != nil {
		t.Fatal(err)
	}
	sess2, sid2, err := mgr.FromOrCreate("guild-A", noopMsg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sid1 != sid2 || sess1 != sess2 {
		t.Fatal("repeated FromOrCreate for same guild must return same session")
	}

	// Defaults should be visible via Session.Playlists()
	pls := sess1.Playlists()
	if len(pls) != len(builtinDefaults) {
		t.Fatalf("session should see %d default playlists, got %d", len(builtinDefaults), len(pls))
	}
}
