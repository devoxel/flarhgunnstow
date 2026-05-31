package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/disgoorg/disgo/voice"
)

var (
	ErrSessionExists       = errors.New("session already exists")
	ErrSessionDoesNotExist = errors.New("session does not exist")
)

// SessionManager owns the registry of active (in-memory) sessions and the
// persistent Store. Sessions are ephemeral; the Store survives restarts.
type SessionManager struct {
	mu sync.Mutex

	store *Store

	// guildLookup maps a Discord guild ID to its current session ID.
	guildLookup map[string]string

	// sessions maps a session ID to its in-memory state.
	sessions map[string]*Session
}

// NewSessionManager constructs a manager backed by the given Store.
func NewSessionManager(store *Store) *SessionManager {
	return &SessionManager{
		store:       store,
		guildLookup: map[string]string{},
		sessions:    map[string]*Session{},
	}
}

// FromOrCreate returns the existing session for a guild or creates a new one.
// On creation it ensures the guild is registered in the Store and clones the
// default playlists into it (idempotent on subsequent calls for other guilds).
//
// Callers always pass fresh msg / joinVoice closures bound to the originating
// Discord event; we update those on every call so the latest channel context
// is used.
func (s *SessionManager) FromOrCreate(guildID string,
	msg func(msg string) error, joinVoice func() (voice.Conn, error)) (*Session, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sID, ok := s.guildLookup[guildID]
	if !ok {
		if err := s.store.EnsureGuild(guildID); err != nil {
			return nil, "", fmt.Errorf("FromOrCreate: ensure guild: %w", err)
		}
		if err := s.store.CloneDefaultPlaylists(guildID); err != nil {
			return nil, "", fmt.Errorf("FromOrCreate: clone defaults: %w", err)
		}

		sID = s.generateSID()
		s.sessions[sID] = newSession(s.store, guildID)
		s.guildLookup[guildID] = sID
	}

	state, ok := s.sessions[sID]
	if !ok {
		return nil, "", fmt.Errorf("FromOrCreate: no state for session id %v", sID)
	}

	state.msg = msg
	state.joinVoice = joinVoice
	return state, sID, nil
}

// FromGuild returns the active session for a guild, if any.
func (s *SessionManager) FromGuild(guildID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sID, ok := s.guildLookup[guildID]
	if !ok {
		return nil, ErrSessionDoesNotExist
	}
	state, ok := s.sessions[sID]
	if !ok {
		return nil, fmt.Errorf("FromGuild: no state for session id %v", sID)
	}
	return state, nil
}

func (s *SessionManager) Exists(sID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.sessions[sID]
	return ok
}

func (s *SessionManager) GetState(sID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.sessions[sID]
	if !ok {
		return nil, errors.New("invalid id")
	}
	return state, nil
}

// SetPlaylist is a convenience wrapper used by the WebSocket handler.
func (s *SessionManager) SetPlaylist(id, title string) error {
	state, err := s.GetState(id)
	if err != nil {
		return err
	}
	state.SetPlaylist(title)
	return nil
}

// generateSID returns a cryptographically random session token. Caller must
// hold s.mu so the uniqueness check is race-free.
func (s *SessionManager) generateSID() string {
	for {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			// crypto/rand failure is catastrophic; fall back to a panic so
			// the operator notices rather than silently issuing weak IDs.
			panic(fmt.Errorf("generateSID: %w", err))
		}
		sid := hex.EncodeToString(b)
		if _, exists := s.sessions[sid]; !exists {
			return sid
		}
	}
}
