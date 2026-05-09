package main

import (
	"fmt"
	"log"
	"sort"
	"sync"

	"github.com/disgoorg/disgo/voice"
)

// GuildPlaylist store all playlists sorted (but with O(n logn) inserts)
// This is no thread safe (although this should not be relevant since they are only used
// by guildState, which locks itself anyway).
//
// since the web client curls VERY regularly it makes sense to implement it this
// way. we could also have some kind of cache and use a sorted set, but since
// we don't expect too many playlists to be added his is okay.
type GuildPlaylist struct {
	playlists []*Playlist
	keys      map[string]struct{}
}

func newGuildPlaylists() *GuildPlaylist {
	return &GuildPlaylist{
		keys:      map[string]struct{}{},
		playlists: []*Playlist{},
	}
}

func (gp *GuildPlaylist) GetAll() []*Playlist {
	return gp.playlists
}

func (gp *GuildPlaylist) get(t string) (int, error) {
	_, exists := gp.keys[t]
	if !exists {
		return -1, ErrGuildPlaylistDoesNotExist
	}

	i := sort.Search(len(gp.playlists), func(i int) bool {
		return gp.playlists[i].Title >= t
	})

	if i >= len(gp.playlists) || gp.playlists[i].Title != t {
		// key's value does not reflect reality
		log.Printf("ERROR: data integrity issue: playlist '%s' did not exist in data but has key", t)
		delete(gp.keys, t)
		return -1, ErrGuildPlaylistDoesNotExist
	}

	return i, nil

}

func (gp *GuildPlaylist) Get(t string) (*Playlist, error) {
	i, err := gp.get(t)
	if err != nil {
		return nil, err
	}
	return gp.playlists[i], nil
}

func (gp *GuildPlaylist) Insert(pl *Playlist) error {
	_, exists := gp.keys[pl.Title]
	if exists {
		return ErrGuildPlaylistExists
	}

	gp.keys[pl.Title] = struct{}{}
	gp.playlists = append(gp.playlists, pl)
	gp.sort()

	return nil
}

func (gp *GuildPlaylist) Remove(t string) error {
	_, exists := gp.keys[t]
	if !exists {
		return ErrGuildPlaylistDoesNotExist
	}

	delete(gp.keys, t)

	i, err := gp.get(t)
	if err != nil {
		return err
	}

	l := len(gp.playlists)
	copy(gp.playlists[i:], gp.playlists[i+1:])
	gp.playlists[l-1] = nil
	gp.playlists = gp.playlists[:l-1]
	return nil
}

func (gp *GuildPlaylist) sort() {
	sort.Slice(gp.playlists, func(i, j int) bool {
		return gp.playlists[i].Title < gp.playlists[j].Title
	})
}

type Session struct {
	sync.Mutex

	playlists *GuildPlaylist

	msg       func(msg string) error
	joinVoice func() (voice.Conn, error)
	p         *Player
}

func newSession() *Session {
	playlists := newGuildPlaylists()

	for _, sample := range samplePlaylists {
		newPl, err := NewPlaylist(sample.Title, sample.Category, sample.Tracks)
		if err != nil {
			log.Fatalf("could not init guildState from sample playlist: %v", err)
		}
		playlists.Insert(newPl)
	}

	// TODO: persist playlists
	return &Session{
		p:         NewPlayer(),
		playlists: playlists,
	}
}

func (gs *Session) SetPlaylist(title string) {
	gs.Lock()
	defer gs.Unlock()

	pl, err := gs.playlists.Get(title)
	if err != nil {
		log.Printf("SetPlaylist: cannot find playlist: %v", err) // XXX Debug
		gs.msg(fmt.Sprintf("Sorry, I can't find the playlist %#v.", title))
		return
	}

	if err := gs.p.SetPlaylist(pl); err != nil {
		log.Printf("SetPlaylist: cannot set: %v", err)
		msg := fmt.Sprintf("Couldn't set your playlist. Here's the debug output: %#v", err)
		gs.msg(msg)
		return
	}

	// Signal that we want to join the voice channel and start playing.
	gs.p.Start(gs.msg, gs.joinVoice)
}

func (gs *Session) QueueSingle(search string) (Track, error) {
	gs.Lock()
	defer gs.Unlock()

	track, err := gs.p.QueueSingle(search)
	if err != nil {
		log.Printf("QueueSingle(%s) error: %v", search, err)
		msg := fmt.Sprintf("Oops! Flargunnstow failed at the modest tasks that was his charge. Debug: %#v", err)
		gs.msg(msg)
		return Track{}, err
	}

	// Signal that we want to join the voice channel and start playing.
	gs.p.Start(gs.msg, gs.joinVoice)
	return track, nil
}

func (gs *Session) Playing() (Track, []Track) {
	return gs.p.Playing()
}

func (gs *Session) Skip() {
	gs.p.Skip()
}

func (gs *Session) Stop() {
	gs.p.Stop()
}

func (gs *Session) Playlists() []*Playlist {
	gs.Lock()
	defer gs.Unlock()

	return gs.playlists.GetAll()
}

func (gs *Session) AddPlaylist(p *Playlist) error {
	gs.Lock()
	defer gs.Unlock()

	return gs.playlists.Insert(p)
}

func (gs *Session) RemovePlaylist(title string) error {
	gs.Lock()
	defer gs.Unlock()

	return gs.playlists.Remove(title)
}
