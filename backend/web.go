package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

func writeError(where string, w http.ResponseWriter, _ *http.Request, err error, c int) {
	log.Printf("%s: %v", where, err)
	http.Error(w, err.Error(), c)
}

type wsMsg struct {
	Message string `json:"message"`

	// StatusCheck
	Status           string      `json:"status,omitempty"`
	Playlists        []*Playlist `json:"playlists,omitempty"`
	CurrentlyPlaying Track       `json:"playing"`
	CurrentPlaylist  []Track     `json:"current_playlist,omitempty"`

	// Push metadata: monotonically increasing generation counter and a
	// compact SHA-256 prefix so the client can detect gaps / corruption.
	Generation uint64 `json:"gen,omitempty"`
	StateHash  string `json:"hash,omitempty"`

	// MusicSelect
	Title string `json:"title,omitempty"`

	// MusicSkip
	//  Empty.
}

// sessionSnapshot builds a full StatusCheckResponse from the current session
// state. This is used both for heartbeat responses and push broadcasts.
func sessionSnapshot(st *Session) (wsMsg, error) {
	playing, playlist := st.Playing()
	playlists := st.Playlists()

	return wsMsg{
		Message:          "StatusCheckResponse",
		Status:           "Verified",
		Playlists:        playlists,
		CurrentlyPlaying: playing,
		CurrentPlaylist:  playlist,
	}, nil
}

func writeMsg(c *websocket.Conn, msg wsMsg) error {
	w, err := c.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(w).Encode(msg); err != nil {
		return err
	}
	return nil
}

// serveSession handles a single WebSocket connection for the given session id.
// It splits into two goroutines:
//   - a read loop that processes incoming client messages
//   - a write loop that subscribes to the session broadcaster for push updates
//
// The broadcaster pushes on every state change; the client sends StatusCheck
// as a heartbeat every ~30s for connection health and self-correction.
func serveSession(c *websocket.Conn, id string, sm *SessionManager) {
	st, err := sm.GetState(id)
	if err != nil {
		log.Printf("serveSession: GetState(%s): %v", id, err)
		c.Close()
		return
	}

	// Subscribe to push broadcasts.
	pushCh := st.Broadcaster().Subscribe()

	var wg sync.WaitGroup
	wg.Add(2)

	// Write loop: push updates from the broadcaster + initial snapshot.
	go func() {
		defer wg.Done()
		defer func() {
			st.Broadcaster().Unsubscribe(pushCh)
			// Close the connection so the read loop also exits.
			c.Close()
		}()

		// Send initial snapshot on connect.
		snap, err := sessionSnapshot(st)
		if err != nil {
			log.Printf("serveSession: initial snapshot: %v", err)
			return
		}
		if err := writeMsg(c, snap); err != nil {
			return
		}

		for payload := range pushCh {
			w, err := c.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(payload); err != nil {
				return
			}
		}
	}()

	// Read loop: process incoming client messages.
	go func() {
		defer wg.Done()
		defer c.Close()

		// Heartbeat ticker: send StatusCheck every 30s so the connection
		// stays alive and the client can self-correct if it misses a push.
		heartbeat := time.NewTicker(30 * time.Second)
		defer heartbeat.Stop()

		// Pump heartbeat ticks into a channel so we can select on them.
		go func() {
			for range heartbeat.C {
				// Send a StatusCheck to our own handler via a synthetic
				// local call — cheaper than going through the read path.
				snap, err := sessionSnapshot(st)
				if err != nil {
					continue
				}
				if err := writeMsg(c, snap); err != nil {
					return
				}
			}
		}()

		for {
			messageType, r, err := c.NextReader()
			if err != nil {
				log.Printf("serveSession: read error: %v", err)
				return
			}

			if messageType != websocket.TextMessage {
				log.Println("serveSession: bad message type")
				return
			}

			var req wsMsg
			if err := json.NewDecoder(r).Decode(&req); err != nil {
				log.Printf("serveSession: Decode: %v", err)
				return
			}

			switch req.Message {
			case "StatusCheck":
				// Client is requesting a full state snapshot (heartbeat / self-correct).
				snap, err := sessionSnapshot(st)
				if err != nil {
					log.Printf("serveSession: StatusCheck: %v", err)
					return
				}
				if err := writeMsg(c, snap); err != nil {
					return
				}
			case "MusicSelect":
				st.SetPlaylist(req.Title)
			case "MusicSkip":
				st.Skip()
			default:
				log.Printf("serveSession: unknown message %q", req.Message)
			}
		}
	}()

	wg.Wait()
}

func websocketHandler(ongoingSessions *SessionManager) func(w http.ResponseWriter, r *http.Request) {
	var originFunc func(r *http.Request) bool = nil
	if debugMode {
		originFunc = func(r *http.Request) bool { return true }
	}
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     originFunc,
	}

	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		param, ok := q["s"]
		if !ok || len(param) != 1 || param[0] == "" {
			writeError("/ws", w, r, fmt.Errorf("no session id:"), 500)
			return
		}

		id := param[0]

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			writeError("ws", w, r, err, 500)
			return
		}

		serveSession(conn, id, ongoingSessions)
	}
}

func handlerInit(ongoingSessions *SessionManager) {
	frontendPath := path.Join(runningDir, "frontend/dist")
	index := path.Join(frontendPath, "index.html")
	_, err := os.Stat(index)
	if err != nil {
		log.Fatalf("cannot stat frontend path %v: %v", index, err)
	}

	staticHandler := http.FileServer(http.Dir(frontendPath))

	http.HandleFunc("/ws", websocketHandler(ongoingSessions))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// TODO: Should probably use something cached.
		// XXX: Remove hardcoded URL.
		staticHandler.ServeHTTP(w, r)
	})
}
