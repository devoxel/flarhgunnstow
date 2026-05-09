package spotify

// Credit to https://github.com/rapito/go-spotify for the inital spotify interaction code

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	ACCOUNTS_URL = "https://accounts.spotify.com/api/token"
)

var spotifyHTTPClient = &http.Client{Timeout: 10 * time.Second}

type SpotifyAuthResponse struct {
	AccessToken string `json:"access_token,omitempty"`
	TokenType   string `json:"token_type,omitempty"`
	ExpiresIn   int    `json:"expires_in,omitempty"`
}

// Client struct which we use to wrap our request operations.
type Client struct {
	ClientID     string
	ClientSecret string
	accessToken  string
}

func (s *Client) keys() string {
	d := fmt.Sprintf("%v:%v", s.ClientID, s.ClientSecret)
	return base64.StdEncoding.EncodeToString([]byte(d))
}

func (s *Client) auth() string {
	return fmt.Sprintf("Bearer %v", s.accessToken)
}

// Authorizes your application against Client
func (s *Client) Authorize() error {
	// Get Encoded Access Keys for Authentication
	auth := fmt.Sprintf("Basic %s", s.keys())

	// Create a new request to get our access_token
	// and send our Keys on Authorization Header
	body := strings.NewReader("grant_type=client_credentials")
	req, err := http.NewRequest("POST", ACCOUNTS_URL, body)
	if err != nil {
		return fmt.Errorf("Authorize: error building API request: %w", err)
	}

	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := spotifyHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("Authorize: error sending API request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return fmt.Errorf("Authorize: invalid auth: response: %v", res) // XXX: debug logs
	}

	var m SpotifyAuthResponse
	d := json.NewDecoder(res.Body)
	if err := d.Decode(&m); err != nil {
		return fmt.Errorf("Authorize: error decoding json %w", err)
	}

	s.accessToken = m.AccessToken
	return nil
}

// GetPlaylist gets a Spotify playlist.
func (s *Client) GetPlaylist(url string) (*FullPlaylist, error) {
	const URL = "https://api.spotify.com/v1/playlists/%s"

	// WOW! Good URL cleaning! Nice!
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")
	id := strings.TrimPrefix(url, "open.spotify.com/playlist/")

	playlistURL := fmt.Sprintf(URL, id)
	// log.Printf("getting: %v", playlistURL) XXX: Debug

	pl := &FullPlaylist{}
	if err := s.get(playlistURL, pl); err != nil {
		return nil, err
	}

	return pl, nil
}

// GetUserPlaylists gets all Spotify playlist for a specific user
func (s *Client) GetUserPlaylists(id string) (*SimplePlaylistPage, error) {
	const URL = "https://api.spotify.com/v1/users/%s/playlists"

	playlistURL := fmt.Sprintf(URL, id)
	log.Printf("getting: %v", playlistURL)

	pl := &SimplePlaylistPage{}
	if err := s.get(playlistURL, pl); err != nil {
		return nil, err
	}

	return pl, nil
}

func (s *Client) get(url string, result interface{}) error {
	// XXX: add retries
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("get: error building API request: %w", err)
	}

	req.Header.Set("Authorization", s.auth())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("get: error sending API request: %w", err)
	}

	if res.StatusCode != 200 {
		return fmt.Errorf("get: API error: %v", res) // XXX: debug logs
	}

	d := json.NewDecoder(res.Body)

	if err := d.Decode(&result); err != nil {
		return fmt.Errorf("get: error decoding json: %w", err)
	}

	return nil
}
