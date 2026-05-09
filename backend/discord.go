package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

type DiscordBot struct {
	sessions *SessionManager
}

func (s *DiscordBot) incomingMessage(e *events.MessageCreate) {
	s.handleMessage(e)
}

func (s *DiscordBot) handleMessage(e *events.MessageCreate) {
	const prefix = ";"

	if !strings.HasPrefix(e.Message.Content, prefix) {
		return
	}

	// Ignore DMs
	if e.GuildID == nil {
		return
	}

	cmd := strings.Fields(strings.TrimPrefix(e.Message.Content, prefix))
	if len(cmd) == 0 {
		return
	}
	cmd[0] = strings.ToLower(cmd[0])

	switch cmd[0] {
	case "create", "start":
		s.handleCreate(e)
	case "stop":
		s.handleStop(e)
	case "q", "queue":
		s.handleQueue(e)
	case "play", "p":
		s.handlePlay(e, strings.Join(cmd[1:], " "))
	case "skip", "s":
		s.handleSkip(e)
	case "add_playlist":
		if len(cmd) < 2 {
			s.sendErrorMsg(e, errors.New("remind devoxel to implement this"))
			return
		}
		s.handleAdd(e, cmd[1], "misc", []Track{})
	case "delete_playlist":
		if len(cmd) < 2 {
			return
		}
		s.handleDelete(e, cmd[1])
	}
}

func (s *DiscordBot) handlePlay(e *events.MessageCreate, search string) {
	gs, _, err := s.getOrCreateSession(e)
	if err != nil {
		s.sendErrorMsg(e, err)
		return
	}

	track, err := gs.QueueSingle(search)
	if err != nil {
		s.sendErrorMsg(e, err)
		return
	}

	msg := discord.NewMessageCreate().WithEmbeds(
		discord.NewEmbed().
			WithColor(3447003).
			AddField("Queued", track.Name, false),
	)
	if _, err = e.Client().Rest.CreateMessage(e.ChannelID, msg); err != nil {
		log.Printf("handlePlay: %v", err)
	}
}

func (s *DiscordBot) handleAdd(e *events.MessageCreate, name, category string, tracks []Track) {
	gs, err := s.sessions.FromGuild(e.GuildID.String())
	if err != nil {
		s.sendErrorMsg(e, err)
		return
	}

	pl, err := NewPlaylist(name, category, tracks)
	if err != nil {
		s.sendErrorMsg(e, err)
		return
	}

	if err := gs.AddPlaylist(pl); err != nil {
		s.sendErrorMsg(e, err)
	}
}

func (s *DiscordBot) handleDelete(e *events.MessageCreate, name string) {
	// TODO: implement
}

func (s *DiscordBot) sendErrorMsg(e *events.MessageCreate, err error) {
	log.Printf("sending err: %v", err)
	msg := discord.NewMessageCreate().WithContent(err.Error())
	if _, sErr := e.Client().Rest.CreateMessage(e.ChannelID, msg); sErr != nil {
		log.Printf("cannot send error message: %v", sErr)
	}
}

func (s *DiscordBot) getSenderVoiceChannel(e *events.MessageCreate) (snowflake.ID, error) {
	vs, ok := e.Client().Caches.VoiceState(*e.GuildID, e.Message.Author.ID)
	if !ok || vs.ChannelID == nil {
		return 0, errors.New("you must be in a voice channel")
	}
	return *vs.ChannelID, nil
}

func (s *DiscordBot) handleQueue(e *events.MessageCreate) {
	gs, err := s.sessions.FromGuild(e.GuildID.String())
	if err == ErrSessionDoesNotExist {
		s.sendMsg(e, "i'm not playing anything")
		return
	} else if err != nil {
		s.sendErrorMsg(e, err)
		return
	}

	tracks := []string{}
	playing, playlist := gs.Playing()
	for _, t := range playlist {
		if t.Equal(playing) {
			tracks = append(tracks, "+  "+t.Name+" (now playing)")
		} else {
			tracks = append(tracks, "-  "+t.Name)
		}
	}

	s.sendMsg(e, "```\n"+strings.Join(tracks, "\n")+"\n```")
}

func (s *DiscordBot) sendMsg(e *events.MessageCreate, msg string) {
	if _, err := e.Client().Rest.CreateMessage(e.ChannelID,
		discord.NewMessageCreate().WithContent(msg)); err != nil {
		log.Printf("sendMsg: %v", err)
	}
}

func (s *DiscordBot) partialSendMsg(e *events.MessageCreate) func(string) error {
	channelID := e.ChannelID
	rest := e.Client().Rest
	return func(m string) error {
		_, err := rest.CreateMessage(channelID, discord.NewMessageCreate().WithContent(m))
		return err
	}
}

func (s *DiscordBot) partialJoinVoice(e *events.MessageCreate) (func() (voice.Conn, error), error) {
	channelID, err := s.getSenderVoiceChannel(e)
	if err != nil {
		return nil, err
	}
	guildID := *e.GuildID
	vm := e.Client().VoiceManager

	return func() (voice.Conn, error) {
		// TODO: track active sessions properly to avoid recreating
		conn := vm.CreateConn(guildID)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := conn.Open(ctx, channelID, false, true); err != nil {
			return nil, fmt.Errorf("joining voice: %w", err)
		}
		return conn, nil
	}, nil
}

func (s *DiscordBot) getOrCreateSession(e *events.MessageCreate) (*Session, string, error) {
	joinVoice, err := s.partialJoinVoice(e)
	if err != nil {
		return nil, "", err
	}
	return s.sessions.FromOrCreate(e.GuildID.String(), s.partialSendMsg(e), joinVoice)
}

func (s *DiscordBot) handleCreate(e *events.MessageCreate) {
	_, sessionToken, err := s.getOrCreateSession(e)
	if err != nil {
		s.sendErrorMsg(e, err)
		return
	}
	s.sendMsg(e, fmt.Sprintf("join here: %s/?s=%s", siteURL, sessionToken))
}

func (s *DiscordBot) handleStop(e *events.MessageCreate) {
	gs, err := s.sessions.FromGuild(e.GuildID.String())
	if err != nil {
		s.sendErrorMsg(e, err)
		return
	}
	gs.Stop()
	s.sendMsg(e, "bye! see you soon :)")
}

func (s *DiscordBot) handleSkip(e *events.MessageCreate) {
	gs, err := s.sessions.FromGuild(e.GuildID.String())
	if err != nil {
		s.sendErrorMsg(e, err)
		return
	}
	gs.Skip()
}
