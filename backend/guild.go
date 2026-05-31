package main

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/disgoorg/disgo/voice"
)

var (
	ErrGuildPlaylistExists       = errors.New("a playlist with that title already exists")
	ErrGuildPlaylistDoesNotExist = errors.New("a playlist with that title does not exist")
)

// Session is the per-guild in-memory state. Playlists are sourced from the
// Store on demand so the DB is the single source of truth; the Session only
// holds ephemeral things (the active Player and Discord-event-derived
// callbacks).
type Session struct {
	sync.Mutex

	guildID string
	store   *Store

	msg       func(msg string) error
	joinVoice func() (voice.Conn, error)
	p         *Player

	broadcaster *SessionBroadcaster
}

func newSession(store *Store, guildID string) *Session {
	return &Session{
		guildID:     guildID,
		store:       store,
		p:           NewPlayer(),
		broadcaster: NewSessionBroadcaster(),
	}
}

func (gs *Session) SetPlaylist(title string) {
	gs.Lock()
	pl, err := gs.store.LoadPlaylistByTitle(gs.guildID, title)
	if err == ErrPlaylistNotFound {
		gs.Unlock()
		log.Printf("SetPlaylist: %q not found for guild %s", title, gs.guildID)
		gs.msg(fmt.Sprintf("Sorry, I can't find the playlist %#v.", title))
		return
	} else if err != nil {
		gs.Unlock()
		log.Printf("SetPlaylist: load: %v", err)
		gs.msg(fmt.Sprintf("Couldn't load that playlist: %v", err))
		return
	}

	if err := gs.p.SetPlaylist(pl); err != nil {
		gs.Unlock()
		log.Printf("SetPlaylist: set: %v", err)
		gs.msg(fmt.Sprintf("Couldn't set your playlist: %v", err))
		return
	}
	gs.p.Start(gs.msg, gs.joinVoice)
	gs.p.onTrackChange = gs.broadcastStateChange
	gs.Unlock()

	// Push update to all WebSocket clients.
	gs.broadcastStateChange()
}

func (gs *Session) QueueSingle(search string) (Track, error) {
	gs.Lock()
	track, err := gs.p.QueueSingle(search)
	if err != nil {
		gs.Unlock()
		log.Printf("QueueSingle(%s) error: %v", search, err)
		gs.msg(fmt.Sprintf("Oops! Flargunnstow failed at the modest task that was his charge. Debug: %#v", err))
		return Track{}, err
	}
	gs.p.Start(gs.msg, gs.joinVoice)
	gs.p.onTrackChange = gs.broadcastStateChange
	gs.Unlock()

	gs.broadcastStateChange()
	return track, nil
}

func (gs *Session) Playing() (Track, []Track) {
	return gs.p.Playing()
}

func (gs *Session) Skip() {
	gs.p.Skip()
	gs.broadcastStateChange()
}

func (gs *Session) Stop() {
	gs.p.Stop()
	gs.broadcastStateChange()
}

// Playlists returns a freshly-loaded snapshot of the guild's playlists from
// the Store. Each call hits the DB; the WebSocket handler relies on this for
// status check responses.
func (gs *Session) Playlists() []*Playlist {
	pls, err := gs.store.LoadPlaylists(gs.guildID)
	if err != nil {
		log.Printf("Playlists: %v", err)
		return nil
	}
	return pls
}

// AddPlaylist persists a new playlist. Returns ErrGuildPlaylistExists if a
// playlist with the same title already exists for the guild.
func (gs *Session) AddPlaylist(p *Playlist) error {
	if p == nil {
		return errors.New("nil playlist")
	}
	existing, err := gs.store.LoadPlaylistByTitle(gs.guildID, p.Title)
	if err == nil && existing != nil {
		return ErrGuildPlaylistExists
	}
	if err != nil && err != ErrPlaylistNotFound {
		return err
	}
	return gs.store.SavePlaylist(gs.guildID, p)
}

// RemovePlaylist deletes a playlist by title. Returns
// ErrGuildPlaylistDoesNotExist if no such playlist is registered.
func (gs *Session) RemovePlaylist(title string) error {
	err := gs.store.DeletePlaylist(gs.guildID, title)
	if err == ErrPlaylistNotFound {
		return ErrGuildPlaylistDoesNotExist
	}
	return err
}

// Broadcaster returns the session broadcaster for push-based WebSocket updates.
func (gs *Session) Broadcaster() *SessionBroadcaster {
	return gs.broadcaster
}

// broadcastStateChange pushes a full state snapshot to all connected WebSocket
// clients. Must be called without holding gs.Lock() (it acquires p.Lock
// indirectly via Playing, and the store lock via Playlists).
func (gs *Session) broadcastStateChange() {
	snap, err := sessionSnapshot(gs)
	if err != nil {
		log.Printf("broadcastStateChange: snapshot: %v", err)
		return
	}
	gs.broadcaster.Broadcast(snap)
}
