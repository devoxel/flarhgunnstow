package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/devoxel/dndmusic/spotify"
	"github.com/disgoorg/disgo/voice"
)

type AudioDownloadManager struct {
	sync.Mutex

	// cache spotify playlists to avoid hitting limits
	playlistCache map[string][]string

	s *spotify.Client
}

func writeJSON(path string, t any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// No reason to be concerned about bytes here for right now.
	e := json.NewEncoder(f)
	e.SetIndent("", "\t")
	if err := e.Encode(t); err != nil {
		return err
	}
	return nil
}

func loadJSON(path string, t any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	d := json.NewDecoder(f)
	if err := d.Decode(t); err != nil {
		return err
	}

	return nil
}

func getPlaylistCachePath() string {
	return fmt.Sprintf("%s/playlist_cache.json", videoDir)
}

// flush cache to disk. don't hotload or anything fancy. eventually we could
// use redis or something fancy for the cache, for now just write to disc and we'll read it back
func (adm *AudioDownloadManager) flushCache() error {
	adm.Lock()
	defer adm.Unlock()

	if err := writeJSON(getPlaylistCachePath(), &adm.playlistCache); err != nil {
		return fmt.Errorf("writeJSON(playlistCache): %w", err)
	}

	return nil
}

func (adm *AudioDownloadManager) readCache() error {
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		return fmt.Errorf("creating video dir: %w", err)
	}

	adm.Lock()

	if err := loadJSON(getPlaylistCachePath(), &adm.playlistCache); err != nil {
		if !os.IsNotExist(err) {
			adm.Unlock()
			return fmt.Errorf("loadJSON(discordCache): %w", err)
		}
	}

	adm.Unlock()

	return adm.flushCache()
}

// Initalized in init(), see main.go
var adm *AudioDownloadManager = nil

type Player struct {
	sync.Mutex

	q      *PlayerQ
	audio  chan []byte
	signal chan PlayerSignal

	exit chan struct{}
}

func NewPlayer() *Player {
	return &Player{}
}

func (p *Player) Start(msg func(msg string) error, joinVoice func() (voice.Conn, error)) {
	log.Println("Start(): starting...") // XXX DEBUG
	p.Lock()
	defer p.Unlock()
	if p.signal != nil {
		p.signal <- SigReload
		return
	}

	if p.q == nil {
		p.q = NewPlayerQ()
	}
	p.signal = make(chan PlayerSignal)
	go p.PlayLoop(msg, joinVoice)
}

// queueLocalFile looks for a file in videoDir whose name contains search
// (case-insensitive). Returns the matched Track or an error.
func queueLocalFile(search string) (Track, error) {
	entries, err := os.ReadDir(videoDir)
	if err != nil {
		return Track{}, fmt.Errorf("reading video dir: %w", err)
	}
	lower := strings.ToLower(search)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.Contains(strings.ToLower(e.Name()), lower) {
			path := filepath.Join(videoDir, e.Name())
			name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			return Track{Name: name, Path: path}, nil
		}
	}
	return Track{}, fmt.Errorf("no local file found matching %q in %s", search, videoDir)
}

func (p *Player) QueueSingle(search string) (Track, error) {
	log.Printf("QueueSingle: queueing %s", search)

	// If search looks like an absolute path or a relative path to an existing
	// file, treat it as a local file directly.
	var track Track
	var err error
	if filepath.IsAbs(search) {
		if _, statErr := os.Stat(search); statErr == nil {
			name := strings.TrimSuffix(filepath.Base(search), filepath.Ext(search))
			track = Track{Name: name, Path: search}
		} else {
			return Track{}, fmt.Errorf("local file not found: %s", search)
		}
	} else if videoDir != "" && videoDir != "." {
		// Try local file search first; fall back to youtube-dl on failure.
		track, err = queueLocalFile(search)
		if err != nil {
			log.Printf("QueueSingle: local lookup failed (%v), trying youtube-dl", err)
			track, err = adm.DLInfo(search)
			if err != nil {
				return Track{}, err
			}
		}
	} else {
		track, err = adm.DLInfo(search)
		if err != nil {
			return Track{}, err
		}
	}

	p.Lock()
	if p.q == nil {
		p.q = NewPlayerQ()
	}
	p.q.Append(track)
	p.Unlock()

	return track, nil
}

func (p *Player) SetPlaylist(playlist *Playlist) error {
	p.Lock()

	// Set current song to top of playlist.
	p.q = NewPlayerQFromPlaylist(playlist.Tracks)

	p.Unlock()
	return nil
}

func (p *Player) Playing() (Track, []Track) {
	p.Lock()
	defer p.Unlock()

	if p.signal != nil {
		return Track{}, []Track{}
	}

	t, pl, err := p.q.Current()
	if err != nil {
		return Track{}, []Track{}
	}

	return t, pl
}

func (p *Player) Skip() {
	p.signal <- SigSkip
}

func (p *Player) Stop() {
	p.signal <- SigStop
}
