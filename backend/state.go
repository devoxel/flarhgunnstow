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
	ErrGuildPlaylistExists       = errors.New("a playlist with that title already exists")
	ErrGuildPlaylistDoesNotExist = errors.New("a playlist with that title does not exist")
)

type SessionManager struct {
	mu sync.Mutex

	// guildLookup provides a way to ongoing sessions for a guild
	//   i.e., map[guild id] -> session id
	// This is used when we get a discord command, to ensure we modify
	// the session that belongs to that guild
	guildLookup map[string]string

	// sessions contains all ongoing discord sessions
	//   i.e., map[session id] -> state
	sessions map[string]*Session
}

var ErrSessionExists = errors.New("session already exists")
var ErrSessionDoesNotExist = errors.New("session does not exist")

func (s *SessionManager) FromOrCreate(guildID string,
	msg func(msg string) error, joinVoice func() (voice.Conn, error)) (*Session, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sID, ok := s.guildLookup[guildID]
	if !ok {
		// XXX: WE NEED TO PERSIST GUILDS HERE!! SUPER MEGA IMPORTANT!!!
		sID = generateSID(s)

		state := newSession()
		s.sessions[sID] = state
		s.guildLookup[guildID] = sID
	}

	state, ok := s.sessions[sID]
	if !ok {
		return nil, "", fmt.Errorf("Create: no corresponding guild state for session id %v", sID)
	}

	state.msg = msg
	state.joinVoice = joinVoice

	return state, sID, nil
}

func (s *SessionManager) FromGuild(guildID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sID, exists := s.guildLookup[guildID]
	if !exists {
		return nil, ErrSessionDoesNotExist
	}

	state, exists := s.sessions[sID]
	if !exists {
		return nil, fmt.Errorf("FromGuild: no corresponding guild state for session id %v", sID)
	}
	return state, nil
}

func (s *SessionManager) Exists(sID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.sessions[sID]
	return exists
}

func (s *SessionManager) GetState(sID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, exists := s.sessions[sID]
	if !exists {
		return nil, errors.New("invalid id")
	}
	return state, nil
}

func (s *SessionManager) SetPlaylist(id, url string) error {
	// no lock here, its intentional :)
	state, err := s.GetState(id)
	if err != nil {
		return err
	}
	state.SetPlaylist(url)
	return nil
}

func generateSID(_ *SessionManager) string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
