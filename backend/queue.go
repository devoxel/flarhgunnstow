package main

import (
	"errors"
	"math/rand"
	"sync"
)

type SigType int

const (
	SigTypeReload = iota
	SigTypeSkip
	SigTypeStop
	SigTypeErr
)

type Track struct {
	Name     string `json:"name,omitempty"`
	Uploader string `json:"uploader,omitempty"`
	URL      string `json:"url,omitempty"`
	Path     string `json:"path,omitempty"`
	AddedBy  string `json:"added_by,omitempty"`
}

func (t Track) Equal(o Track) bool {
	if t.Path != "" {
		return t.Path == o.Path
	}
	return t.URL == o.URL
}

type Playlist struct {
	Title    string `json:"title,omitempty"`
	Category string `json:"category,omitempty"`

	Tracks []Track `json:"tracks,omitempty"`
}

func NewPlaylist(title string, category string, tracks []Track) (*Playlist, error) {
	if title == "" {
		return nil, errors.New("empty name")
	}

	if category == "" {
		return nil, errors.New("empty category")
	}

	return &Playlist{
		Title:    title,
		Category: category,
		Tracks:   tracks,
	}, nil
}

func NewPlaylistFromSpotifyURL(title string, category string, url string) (*Playlist, error) {
	if url == "" {
		return nil, errors.New("empty url")
	}

	/* XXX: FIXME
	tracks, err := adm.DownloadSpotifyPlaylist(url)
	if err != nil {
		return nil, fmt.Errorf("can't download playlist: %w", err)
	}
	return NewPlaylist(title, category, tracks)
	*/
	return nil, errors.New("not implemented")

}

func (p Playlist) Shuffle() {
	l := p.Tracks
	rand.Shuffle(len(l), func(i, j int) {
		l[i], l[j] = l[j], l[i]
	})
}

type PlayerSignal struct {
	Type SigType
	Err  error
}

var (
	SigReload = PlayerSignal{Type: SigTypeReload}
	SigSkip   = PlayerSignal{Type: SigTypeSkip}
	SigStop   = PlayerSignal{Type: SigTypeStop}
)

func SigErr(err error) PlayerSignal {
	return PlayerSignal{
		Type: SigTypeErr,
		Err:  err,
	}
}

var ErrNoSongs = errors.New("no songs in player queue!")

// PlayerQ is the playlist type used by a player.
//
// A player will contain one instance of this playlist, which will
// be used to add / remove tracks.
//
// In general, a PlayerQ can be modified at any time but
// this will not affect the player's current state.
//
// Later when adding balanced Playlist we should be able to
// replace this using an interface to have a BalancedPlayerQ
type PlayerQ struct {
	sync.Mutex

	autoClear bool
	current   int
	playlist  []Track
}

func NewPlayerQ() *PlayerQ {
	return &PlayerQ{
		autoClear: true,
		current:   0,
		playlist:  []Track{},
	}
}

func NewPlayerQFromPlaylist(from []Track) *PlayerQ {
	return &PlayerQ{
		autoClear: false,
		current:   0,
		playlist:  from,
	}
}

func (p *PlayerQ) ToggleShuffle() {
	p.Lock()
	defer p.Unlock()
	p.autoClear = !p.autoClear
}

func (p *PlayerQ) Len() int {
	return len(p.playlist)
}

func (p *PlayerQ) Append(t Track) {
	p.Lock()
	defer p.Unlock()

	p.playlist = append(p.playlist, t)
}

func (p *PlayerQ) Insert(idx int, t Track) error {
	p.Lock()
	defer p.Unlock()
	if idx < 0 {
		return errors.New("index cannot be below zero")
	}

	if idx >= len(p.playlist) {
		p.playlist = append(p.playlist, t)
		return nil
	}

	// Use three-index slice to force a new backing array,
	// avoiding aliasing between the left and right sub-slices.
	p.playlist = append(
		append(p.playlist[:idx:idx], t),
		p.playlist[idx:]...,
	)

	return nil
}

func (p *PlayerQ) SkipNext() Track {
	p.Lock()
	defer p.Unlock()
	p.current += 1

	// TODO: Reshuffle here if shuffle is on
	//  This would require turning shuffle into a Q
	//  managed thing.
	if p.current > (len(p.playlist) - 1) {
		p.current = 0

		if p.autoClear {
			p.playlist = []Track{}
			return Track{}
		}
	}

	return p.playlist[p.current]
}

func (p *PlayerQ) Current() (Track, []Track, error) {
	p.Lock()
	defer p.Unlock()
	if len(p.playlist) == 0 {
		return Track{}, []Track{}, ErrNoSongs
	}

	if p.current >= len(p.playlist) {
		return Track{}, []Track{}, errors.New("the queue has been mangled...")
	}

	// Return a copy of the backing slice so callers cannot mutate our queue
	// out from under us once the lock is released.
	cp := make([]Track, len(p.playlist))
	copy(cp, p.playlist)
	return cp[p.current], cp, nil
}
