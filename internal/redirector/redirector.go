package redirector

import (
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
	guildID            string
	channelID          string
	spotifyAccessToken string
	send               chan []int16
	mutex              sync.Mutex
}

func NewRedirector(botSession *discordgo.Session, guildID, channelID, spotifyAccessToken string) (*Redirector, error) {
	librespotArgs[2] = strings.Replace(librespotArgs[2], "{{ .AccessToken }}", spotifyAccessToken, 1)
	librespotArgs[4] = strings.Replace(librespotArgs[4], "{{ .OutputPath }}", librespotOutputPath, 1)

	cmd := exec.Command(librespotPath, librespotArgs...)
	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start librespot: %v", err)
	}

	time.Sleep(10 * time.Second)

	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		output, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("librespot process died within 10 seconds of starting: %v", cmd.ProcessState.String())
		}

		return nil, fmt.Errorf("librespot process died within 10 seconds of starting: %v\n%s", cmd.ProcessState.String(), string(output))
	}

	return &Redirector{
		botSession:         botSession,
		guildID:            guildID,
		channelID:          channelID,
		spotifyAccessToken: spotifyAccessToken,
		send:               make(chan []int16, 2),
	}, nil
}

func (r *Redirector) Clear() error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return clearFifo(librespotOutputPath)
}

func (r *Redirector) Start() error {
	voiceChannel, err := r.botSession.ChannelVoiceJoin(r.guildID, r.channelID, false, true)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(librespotOutputPath, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}

	go dgvoice.SendPCM(voiceChannel, r.send)

	for {
		r.mutex.Lock()
		samples, err := readFifo(file)
		r.mutex.Unlock()

		if err != nil {
			return err
		}

		r.send <- samples
	}
}
