package redirector

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/bwmarrin/dgvoice"
	"github.com/bwmarrin/discordgo"
)

var (
	librespotOutputPath = "/tmp/librespot.out"
	librespotLogPath    = "/tmp/librespot.log"
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
	voiceChannelID     string
	spotifyAccessToken string
	send               chan []int16
	mutex              sync.Mutex
	cmd                *exec.Cmd
	cancel             context.CancelFunc
	ctx                context.Context
}

func NewRedirector(botSession *discordgo.Session, guildID, voiceChannelID, spotifyAccessToken string) (*Redirector, error) {
	log.Printf("Creating redirector for guild %s, voice channel %s", guildID, voiceChannelID)

	tmpl := template.Must(template.New("args").Parse(strings.Join(librespotArgs, " ")))
	var buf strings.Builder
	if err := tmpl.Execute(&buf, struct {
		AccessToken string
		OutputPath  string
	}{
		AccessToken: spotifyAccessToken,
		OutputPath:  librespotOutputPath,
	}); err != nil {
		return nil, fmt.Errorf("failed to execute template: %v", err)
	}
	librespotArgs = strings.Split(buf.String(), " ")

	logFile, err := os.OpenFile(librespotLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %v", err)
	}

	cmd := exec.Command(librespotPath, librespotArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("failed to start librespot: %v", err)
	}
	log.Printf("Started librespot with args %v", librespotArgs)

	ctx, cancel := context.WithCancel(context.Background())

	return &Redirector{
		botSession:         botSession,
		guildID:            guildID,
		voiceChannelID:     voiceChannelID,
		spotifyAccessToken: spotifyAccessToken,
		send:               make(chan []int16, 2),
		cmd:                cmd,
		ctx:                ctx,
		cancel:             cancel,
	}, nil
}

func (r *Redirector) checkLibrespot() error {
	if err := r.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		output, readErr := os.ReadFile(librespotLogPath)
		if readErr != nil {
			return fmt.Errorf("librespot process died: %v (failed to read logs: %v)", err, readErr)
		}
		return fmt.Errorf("librespot process died: %v\n%s", err, string(output))
	}
	return nil
}

func (r *Redirector) Start() error {
	go func() {
		if err := r.healthChecker(); err != nil {
			log.Printf("Health checker failed: %v", err)
		}
	}()

	voiceChannel, err := r.botSession.ChannelVoiceJoin(r.guildID, r.voiceChannelID, false, true)
	if err != nil {
		return err
	}
	r.voiceChannel = voiceChannel
	log.Printf("Joined voice channel %s", r.voiceChannelID)

	cmd := exec.Command("sox",
		"-t", "raw",
		"-c", "2",
		"-r", "44.1k",
		"-e", "signed-integer",
		"-L",
		"-b", "16",
		librespotOutputPath,
		"-t", "raw",
		"-r", "48k",
		"-b", "24",
		"--norm",
		"-",
		"rate -v")

	soxOut, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	go dgvoice.SendPCM(voiceChannel, r.send)

	buffer := make([]byte, 3840)
	for {
		select {
		case <-r.ctx.Done():
			return nil
		default:
			n, err := soxOut.Read(buffer)
			if err == io.EOF {
				return nil
			}
			if err != nil {
				close(r.send)
				return err
			}

			pcmData := make([]int16, n/2)
			for i := 0; i < n; i += 2 {
				pcmData[i/2] = int16(buffer[i]) | (int16(buffer[i+1]) << 8)
			}

			r.send <- pcmData
		}
	}
}

func (r *Redirector) Stop() error {
	r.cancel()
	if r.voiceChannel != nil {
		r.voiceChannel.Disconnect()
	}
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
