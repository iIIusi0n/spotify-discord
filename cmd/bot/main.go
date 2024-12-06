package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"spotify-discord/internal/auth"
	"spotify-discord/internal/redirector"

	"github.com/bwmarrin/discordgo"
)

var (
	oauthAccessURL      = os.Getenv("OAUTH_ACCESS_URL")
	spotifyClientID     = os.Getenv("SPOTIFY_OAUTH_CLIENT_ID")
	spotifyClientSecret = os.Getenv("SPOTIFY_OAUTH_CLIENT_SECRET")
	debugMode           = os.Getenv("DEBUG_MODE") == "true" || os.Getenv("DEBUG_MODE") == "1" || os.Getenv("DEBUG_MODE") == "yes" || os.Getenv("DEBUG_MODE") == "TRUE"
	guildID             = os.Getenv("DISCORD_GUILD_ID")
	botToken            = os.Getenv("DISCORD_BOT_TOKEN")
)

var (
	authorizer *auth.SpotifyAuthorizer
	session    *discordgo.Session
	proxy      *redirector.Redirector
)

var (
	commands = []*discordgo.ApplicationCommand{
		{
			Name:        "join",
			Description: "Join the voice channel",
			Type:        discordgo.ChatApplicationCommand,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:         "channel",
					Description:  "The channel to join",
					Type:         discordgo.ApplicationCommandOptionChannel,
					Required:     true,
					ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildVoice},
				},
			},
		},
		{
			Name:        "leave",
			Description: "Leave the voice channel",
			Type:        discordgo.ChatApplicationCommand,
		},
	}

	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"join": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			data := i.ApplicationCommandData()
			voiceChannelID := data.Options[0].ChannelValue(s)

			if proxy == nil {
				token, err := authorizer.AccessToken()
				if err != nil {
					s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData{
							Content: fmt.Sprintf("Failed to get access token, login to Spotify first: %s", oauthAccessURL),
						},
					})
					return
				}

				if proxy != nil {
					proxy.Stop()
				}

				proxy, err = redirector.NewRedirector(s, guildID, voiceChannelID.ID, token)
				if err != nil {
					s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
						Type: discordgo.InteractionResponseChannelMessageWithSource,
						Data: &discordgo.InteractionResponseData{
							Content: fmt.Sprintf("Failed to start redirector: %v", err),
						},
					})
					return
				}
				go func() {
					if err := proxy.Start(); err != nil {
						log.Printf("Redirector failed to start: %v", err)
					}
				}()
			} else {
				proxy.ChangeVoiceChannel(voiceChannelID.ID)
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Joined voice channel",
				},
			})
		},
		"leave": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			if proxy != nil {
				proxy.LeaveVoiceChannel()
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Left voice channel",
				},
			})
		},
	}
)

func init() {
	authorizer = auth.NewSpotifyAuthorizer(spotifyClientID, spotifyClientSecret, oauthAccessURL, debugMode)
	go authorizer.StartOAuthServer()
}

func init() {
	var err error
	session, err = discordgo.New("Bot " + botToken)
	if err != nil {
		log.Fatalf("Invalid bot parameters: %v", err)
	}
}

func main() {
	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) { log.Println("Bot is up!") })
	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})
	err := session.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}
	defer session.Close()

	createdCommands, err := session.ApplicationCommandBulkOverwrite(session.State.User.ID, guildID, commands)

	if err != nil {
		log.Fatalf("Cannot register commands: %v", err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Gracefully shutting down")

	for _, cmd := range createdCommands {
		err := session.ApplicationCommandDelete(session.State.User.ID, guildID, cmd.ID)
		if err != nil {
			log.Fatalf("Cannot delete %q command: %v", cmd.Name, err)
		}
	}
}
