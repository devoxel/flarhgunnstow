package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/devoxel/dndmusic/spotify"
	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/godave/golibdave"
)

var (
	token         string
	port          int
	runningDir    string
	spotifyID     string
	spotifySecret string
	videoDir      string
	workingDir    string
	siteURL       string
	debugMode     bool
)

func init() {
	flag.StringVar(&token, "t", "", "discord bot auth token")
	flag.StringVar(&siteURL, "url", "", "site url")
	flag.StringVar(&spotifyID, "spotify-id", "", "spotify id")
	flag.StringVar(&spotifySecret, "spotify-secret", "", "spotify secret")
	flag.StringVar(&videoDir, "video-dir", ".", "video-directory")
	flag.StringVar(&workingDir, "working-dir", ".", "working-directory")
	flag.IntVar(&port, "p", 8080, "port to run the discord bot")
	flag.StringVar(&runningDir, "d", "", "running directory")
	flag.BoolVar(&debugMode, "debug", false, "disable xss checks + print more debug")
	flag.StringVar(&dbPath, "db", "", "sqlite database file path (defaults to <running-dir>/flarhgunnstow.db)")
}

var dbPath string

func validateWorkingDir() {
	_, err := os.ReadFile(workingDir + "/cookies.txt")
	if err != nil {
		log.Println("warning: no cookies.txt in working dir — youtube-dl downloads will not work (local files still OK)")
	}
}

func initBot(ongoingSessions *SessionManager) *bot.Client {
	b := &DiscordBot{ongoingSessions}

	client, err := disgo.New(token,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentGuildVoiceStates,
				gateway.IntentMessageContent,
			),
		),
		bot.WithEventListenerFunc(b.incomingMessage),
		bot.WithCacheConfigOpts(
			cache.WithCaches(cache.FlagVoiceStates),
		),
		bot.WithVoiceManagerConfigOpts(
			voice.WithDaveSessionCreateFunc(golibdave.NewSession),
		),
	)
	if err != nil {
		log.Fatal("cannot init discord bot: ", err)
	}

	if err = client.OpenGateway(context.TODO()); err != nil {
		log.Fatal("cannot connect to discord gateway: ", err)
	}

	return client
}

func initADM() {
	adm = &AudioDownloadManager{
		playlistCache: map[string][]string{},
		s:             &spotify.Client{ClientID: spotifyID, ClientSecret: spotifySecret},
	}

	if spotifyID != "" && spotifySecret != "" {
		if err := adm.s.Authorize(); err != nil {
			log.Fatalf("cannot init spotify client: %v", err)
		}
	} else {
		log.Println("no spotify credentials provided, skipping spotify init")
	}

	if err := adm.readCache(); err != nil {
		log.Fatal(err)
	}
}

func main() {
	flag.Parse()
	validateWorkingDir()

	log.Println("starting bot ...")

	initADM()

	log.Println("adm started ...")

	if token == "" {
		log.Fatal("no token provided")
	}

	if siteURL == "" {
		log.Fatal("no site url provided")
	}

	if dbPath == "" {
		dbPath = filepath.Join(runningDir, "flarhgunnstow.db")
	}
	store, err := NewStore(dbPath)
	if err != nil {
		log.Fatalf("cannot open persistence (%s): %v", dbPath, err)
	}
	defer store.Close()

	if err := store.AddLocalDefaults(videoDir); err != nil {
		log.Printf("warning: scanning videoDir for defaults failed: %v", err)
	}

	log.Printf("persistence opened at %s", dbPath)

	ongoingSessions := NewSessionManager(store)

	client := initBot(ongoingSessions)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client.Close(ctx)
	}()

	log.Println("discord initialized ...")
	handlerInit(ongoingSessions)

	go func() {
		log.Printf("hosting web server on port: %v ...", port)
		if err := http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", port), nil); err != nil {
			log.Fatal("error hosting server: ", err)
		}
	}()

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, syscall.SIGTERM)
	<-sc
}
