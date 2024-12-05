package redirector

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/dgvoice"
	"github.com/bwmarrin/discordgo"
)

var (
	librespotOutputPath = "/tmp/librespot.out"
	librespotPath       = "/usr/bin/librespot"
	librespotArgs       = []string{
		"--name", "SpeakerPI",
		"--bitrate", "320",
		"--access-token", "{{ .AccessToken }}",
		"--backend", "pipe",
		"--device", "{{ .OutputPath }}",
		"--format", "S16",
		"--initial-volume", "100",
		"--enable-volume-normalisation",
	}
)

type Redirector struct {
	botSession         *discordgo.Session
	voiceChannel       *discordgo.VoiceConnection
	guildID            string
	channelID          string
	spotifyAccessToken string
	send               chan []int16
	mutex              sync.Mutex
	cmd                *exec.Cmd
	cancel             context.CancelFunc
	ctx                context.Context
}

func NewRedirector(botSession *discordgo.Session, guildID, channelID, spotifyAccessToken string) (*Redirector, error) {
	librespotArgs[2] = strings.Replace(librespotArgs[2], "{{ .AccessToken }}", spotifyAccessToken, 1)
	librespotArgs[4] = strings.Replace(librespotArgs[4], "{{ .OutputPath }}", librespotOutputPath, 1)

	cmd := exec.Command(librespotPath, librespotArgs...)
	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start librespot: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Redirector{
		botSession:         botSession,
		guildID:            guildID,
		channelID:          channelID,
		spotifyAccessToken: spotifyAccessToken,
		send:               make(chan []int16, 2),
		cmd:                cmd,
		ctx:                ctx,
		cancel:             cancel,
	}, nil
}

func (r *Redirector) checkLibrespot() error {
	if err := r.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		output, err := r.cmd.Output()
		if err != nil {
			return fmt.Errorf("librespot process died: %v", err)
		}
		return fmt.Errorf("librespot process died: %v\n%s", err, string(output))
	}
	return nil
}

func (r *Redirector) Clear() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return clearFifo(librespotOutputPath)
}

func (r *Redirector) Start() error {
	go r.healthChecker()

	voiceChannel, err := r.botSession.ChannelVoiceJoin(r.guildID, r.channelID, false, true)
	if err != nil {
		return err
	}
	r.voiceChannel = voiceChannel

	file, err := os.OpenFile(librespotOutputPath, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}

	go dgvoice.SendPCM(voiceChannel, r.send)

	for {
		select {
		case <-r.ctx.Done():
			return nil
		default:
			r.mutex.Lock()
			samples, err := readFifo(file)
			r.mutex.Unlock()

			if err != nil {
				return err
			}

			r.send <- samples
		}
	}
}

func (r *Redirector) Stop() error {
	r.cancel()
	r.voiceChannel.Disconnect()
	return r.cmd.Process.Kill()
}

func (r *Redirector) healthChecker() error {
	for {
		if err := r.checkLibrespot(); err != nil {
			log.Printf("Health check failed: %v", err)
			if stopErr := r.Stop(); stopErr != nil {
				log.Printf("Failed to stop redirector: %v", stopErr)
			}
			return err
		}
		time.Sleep(10 * time.Second)
	}
}
