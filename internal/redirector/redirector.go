package redirector

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/dgvoice"
	"github.com/bwmarrin/discordgo"
)

const (
	librespotOutputPath = "/tmp/librespot.out"
	librespotLogPath    = "/tmp/librespot.log"
	librespotPath       = "/usr/bin/librespot"
)

type LibrespotConfig struct {
	Name            string
	Bitrate         string
	AccessToken     string
	Backend         string
	Device          string
	Format          string
	InitialVolume   string
	VolumeNormalise bool
}

func NewLibrespotConfig(accessToken string) *LibrespotConfig {
	return &LibrespotConfig{
		Name:            "SpeakerPI",
		Bitrate:         "320",
		AccessToken:     accessToken,
		Backend:         "pipe",
		Device:          librespotOutputPath,
		Format:          "S16",
		InitialVolume:   "100",
		VolumeNormalise: true,
	}
}

type Redirector struct {
	botSession   *discordgo.Session
	voiceChannel *discordgo.VoiceConnection
	guildID      string
	channelID    string

	spotifyToken string
	librespotCmd *exec.Cmd

	audioBuffer chan []int16

	ctx       context.Context
	cancel    context.CancelFunc
	pcmCancel context.CancelFunc
	mutex     sync.Mutex
}

func NewRedirector(session *discordgo.Session, guildID, channelID, spotifyToken string) (*Redirector, error) {
	log.Printf("Creating redirector for guild %s, channel %s", guildID, channelID)

	ctx, cancel := context.WithCancel(context.Background())

	r := &Redirector{
		botSession:   session,
		guildID:      guildID,
		channelID:    channelID,
		spotifyToken: spotifyToken,
		audioBuffer:  make(chan []int16, 2),
		ctx:          ctx,
		cancel:       cancel,
	}

	if err := r.startLibrespot(); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Redirector) startLibrespot() error {
	config := NewLibrespotConfig(r.spotifyToken)

	logFile, err := os.OpenFile(librespotLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}

	args := r.buildLibrespotArgs(config)
	cmd := exec.Command(librespotPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start librespot: %v", err)
	}

	r.librespotCmd = cmd
	log.Printf("Started librespot with args %v", args)
	return nil
}

func (r *Redirector) buildLibrespotArgs(config *LibrespotConfig) []string {
	args := []string{
		"--name", config.Name,
		"--bitrate", config.Bitrate,
		"--access-token", config.AccessToken,
		"--backend", config.Backend,
		"--device", config.Device,
		"--format", config.Format,
		"--initial-volume", config.InitialVolume,
	}
	if config.VolumeNormalise {
		args = append(args, "--enable-volume-normalisation")
	}
	return args
}

func (r *Redirector) Start() error {
	pcmCtx, pcmCancel := context.WithCancel(r.ctx)
	r.pcmCancel = pcmCancel

	go r.monitorHealth()

	if err := r.joinVoiceChannel(); err != nil {
		return err
	}

	return r.streamAudio(pcmCtx)
}

func (r *Redirector) joinVoiceChannel() error {
	vc, err := r.botSession.ChannelVoiceJoin(r.guildID, r.channelID, false, true)
	if err != nil {
		return fmt.Errorf("failed to join voice channel: %v", err)
	}

	r.voiceChannel = vc
	log.Printf("Joined voice channel %s", r.channelID)
	return nil
}

func (r *Redirector) streamAudio(ctx context.Context) error {
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
		"-c", "2",
		"-")

	soxOut, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	go r.sendPCM(ctx, r.voiceChannel)

	return r.processAudioStream(soxOut)
}

func (r *Redirector) processAudioStream(soxOut io.ReadCloser) error {
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
				close(r.audioBuffer)
				return err
			}

			pcmData := make([]int16, n/2)
			for i := 0; i < n; i += 2 {
				pcmData[i/2] = int16(buffer[i]) | (int16(buffer[i+1]) << 8)
			}

			r.audioBuffer <- pcmData
		}
	}
}

func (r *Redirector) Stop() {
	r.cancel()
	if r.pcmCancel != nil {
		r.pcmCancel()
	}
	if r.voiceChannel != nil {
		r.voiceChannel.Disconnect()
	}
	if r.librespotCmd != nil && r.librespotCmd.Process != nil {
		r.librespotCmd.Process.Kill()
	}
}

func (r *Redirector) LeaveVoiceChannel() {
	if r.pcmCancel != nil {
		r.pcmCancel()
	}

	if r.voiceChannel != nil {
		r.voiceChannel.Disconnect()
	}
}

func (r *Redirector) monitorHealth() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			if err := r.checkLibrespot(); err != nil {
				log.Printf("Health check failed: %v", err)
				r.Stop()
				return
			}
		}
	}
}

func (r *Redirector) checkLibrespot() error {
	if err := r.librespotCmd.Process.Signal(syscall.Signal(0)); err != nil {
		output, readErr := os.ReadFile(librespotLogPath)
		if readErr != nil {
			return fmt.Errorf("librespot process died: %v (failed to read logs: %v)", err, readErr)
		}
		return fmt.Errorf("librespot process died: %v\n%s", err, string(output))
	}
	return nil
}

func (r *Redirector) ChangeVoiceChannel(channelID string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.pcmCancel != nil {
		r.pcmCancel()
	}

	if r.voiceChannel != nil {
		r.voiceChannel.Disconnect()
	}

	r.channelID = channelID
	if err := r.joinVoiceChannel(); err != nil {
		return err
	}

	pcmCtx, pcmCancel := context.WithCancel(r.ctx)
	r.pcmCancel = pcmCancel

	go r.sendPCM(pcmCtx, r.voiceChannel)

	log.Printf("Changed to voice channel %s", channelID)
	return nil
}

func (r *Redirector) sendPCM(ctx context.Context, vc *discordgo.VoiceConnection) {
	dgvoice.SendPCM(vc, r.audioBuffer)
	<-ctx.Done()
}
