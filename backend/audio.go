// Credits to @github.com/ducc for his work on github.com/ducc/GoMusicBot
// which helped simplify this audio processing from a gigantic shit stack of fuck
// into a somewhat reasonable thing.  They give credit to @github.com/bwmarrin's also.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	"github.com/disgoorg/disgo/voice"
	"github.com/jonas747/ogg"
)

const (
	DLBitrate = "48000K"
)

// PlayLoop manages the Player, grabbing tracks off the Q and decoding them.
//
// PlayLoop handles various signals, like file skipping.
func (p *Player) PlayLoop(msg func(string) error, joinVoice func() (voice.Conn, error)) {
	p.Lock()
	p.playerOn = true
	// Using extra channels prevents ffmpeg stutters from disrupting our output.
	// Why 64? I heard it's 1 stacks worth.
	audio := make(chan []byte, 64)
	p.Unlock()
	defer func() {
		p.Lock()
		p.playerOn = false
		p.Unlock()
	}()

	logErr := func(err error) {
		log.Println("PlayLoop: error: ", err)
		msg(fmt.Sprintf("uh oh: %v", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer cancel()
		if err := p.toDiscord(ctx, audio, joinVoice); err != nil {
			logErr(err)
		}
	}()

	for {
		t, _, err := p.q.Current()
		if err == ErrNoSongs {
			time.Sleep(500 * time.Millisecond)
			continue
		} else if err != nil {
			logErr(err)
			return
		}

		log.Println("PlayLoop: playing track =", t)

		sig, err := p.DecodeTrackLoop(ctx, audio, t.CMD())
		if err != nil {
			logErr(err)
			return
		}

		/* TODO: move this logic to parent, stopping playback should be controlled from coordinater */
		switch sig.Type {
		case SigTypeReload:
			log.Println("got clear")
			continue
		case SigTypeSkip:
			p.q.SkipNext()
			continue
		case SigTypeStop:
			return
		case SigTypeErr:
			logErr(sig.Err)
			return
		default:
			err := errors.New("got unknown signal")
			logErr(err)
			return
		}

		// TODO: should implement a catch here to prevent very fast reloading
	}
}

// DecodeTrackLoop decodes a track (using its CMD) and sends opus packets into
// the audio channel. It also handles signals like reload / skip / etc.
func (p *Player) DecodeTrackLoop(ctx context.Context, audio chan []byte, f *exec.Cmd) (PlayerSignal, error) {
	log.Println("DecodeTrackLoop: starting ", f.Args)
	const ffmpegBuffer = 16384 * 4

	out, err := f.StdoutPipe()
	if err != nil {
		return SigStop, err
	}
	f.Stderr = os.Stderr

	defer func() {
		if f.Process != nil {
			if err := f.Process.Kill(); err != nil {
				log.Printf("DecodeTrackLoop: error killing ffmpeg: %v", err)
			}
			f.Wait() // XXX: may need some escape hatch if we get stuck here.
		}
	}()

	in := bufio.NewReaderSize(out, ffmpegBuffer)
	if err := f.Start(); err != nil {
		return SigStop, err
	}
	decoder := ogg.NewPacketDecoder(ogg.NewDecoder(in))

	skip := 2
	for {
		pkt, _, err := decoder.Decode()
		if err != nil && err != io.EOF {
			return SigStop, fmt.Errorf("error reading ogg: %w", err)
		} else if err == io.EOF {
			return SigSkip, nil
		}

		if skip > 0 {
			skip--
			continue
		}

		select {
		case <-ctx.Done():
			return SigStop, nil
		case in := <-p.signal:
			return in, nil
		case audio <- pkt:
		}
	}
}

// toDiscord handles the discord audio connection, pacing opus frames at 20ms.
func (p *Player) toDiscord(ctx context.Context, audio chan []byte,
	joinVoice func() (voice.Conn, error)) error {
	conn, err := joinVoice()
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn.Close(closeCtx)
	}()

	if err := conn.SetSpeaking(ctx, voice.SpeakingFlagMicrophone); err != nil {
		return err
	}

	// DisGo requires draining incoming UDP packets.
	go func() {
		for {
			if _, err := conn.UDP().ReadPacket(); err != nil {
				return
			}
		}
	}()

	// DisGo does not handle frame timing internally — pace at 20ms per opus frame.
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	var in []byte
	for {
		// Wait for the next audio frame (with auto-disconnect timeout).
		select {
		case <-ctx.Done():
			return nil
		case in = <-audio:
		case <-time.After(time.Second * 120):
			// Haven't received a frame in a long time; auto-disconnect.
			return nil
		}

		// Pace to 20ms cadence before sending.
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return nil
		}

		if _, err := conn.UDP().Write(in); err != nil {
			return fmt.Errorf("couldn't send audio to discord: %w", err)
		}
	}
}
