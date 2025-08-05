package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

type Config struct {
	DiscordToken                       string
	MinecraftAddress                   string
	WhitelistRequestsChannelID         string
	MinecraftDiscordMessengerChannelID string
	ServerStatusChannelID              string
}

type MinecraftServerStatus struct {
	TPS         int
	PlayerCount int
}

type App struct {
	config         Config
	discordSession *discordgo.Session
	minecraftConn  net.Conn
	// minecraftServerStatus MinecraftServerStatus // TODO: add this, easier to manage and formatting is nice :D
}

var (
	minecraftConn    net.Conn
	discordSession   *discordgo.Session
	discordChannelID string
)

func main() {
	app := &App{}

	if err := app.loadConfig(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := app.connectToServices(); err != nil {
		log.Fatalf("Failed to connect services: %v", err)
	}

	defer app.shutdown()

	app.setupDiscordHandlers()
	log.Println("Have setup Discord handlers")
	go app.readMinecraftMessages()

	// Wait for interrupt signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Gracefully shutting down.")
}

func (a *App) loadConfig() error {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	a.config = Config{
		DiscordToken:                       os.Getenv("DiscordToken"),
		MinecraftDiscordMessengerChannelID: os.Getenv("MinecraftDiscordMessengerChannelID"),
		WhitelistRequestsChannelID:         os.Getenv("WhitelistRequestsChannelID"),
		ServerStatusChannelID:              os.Getenv("ServerStatusChannelID"),
		MinecraftAddress:                   "localhost:26644",
	}

	if a.config.DiscordToken == "" {
		return fmt.Errorf("missing required environment variables")
	}

	return nil
}

func (a *App) connectToServices() error {
	// Connect to Discord
	var err error
	a.discordSession, err = discordgo.New("Bot " + a.config.DiscordToken)
	if err != nil {
		return fmt.Errorf("invalid bot parameters: %w", err)
	}

	a.discordSession.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMembers

	if err := a.discordSession.Open(); err != nil {
		return fmt.Errorf("cannot open Discord session: %w", err)
	}

	// Connect to Minecraft server
	a.minecraftConn, err = net.Dial("tcp", a.config.MinecraftAddress)
	if err != nil {
		return fmt.Errorf("failed to connect to Minecraft mod socket: %w", err)
	}
	log.Printf("Connected to Minecraft mod socket on %s", a.config.MinecraftAddress)

	return nil
}

func (a *App) setupDiscordHandlers() {
	a.discordSession.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
		log.Println("Bot is now running. Press CTRL+C to exit.")
	})

	a.discordSession.AddHandler(a.onDiscordMessage)
	a.discordSession.AddHandler(a.onWhitelistModalSubmitted)
	a.discordSession.AddHandler(a.onWhitelistModalResponse)
	a.discordSession.AddHandler(onUserLeft)
	a.discordSession.AddHandler(onWhitelistModalRequested)
}

func (a *App) shutdown() {
	if a.discordSession != nil {
		a.discordSession.Close()
	}
	if a.minecraftConn != nil {
		a.minecraftConn.Close()
	}
}

func onUserLeft(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
	user := m.User.ID
	log.Println("user left" + user)
	removeFromWhitelistJson(user)
}

func (a *App) onDiscordMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}

	if m.Content == "!whitelist" {
		sendWhitelistStarter(s, m.ChannelID)
	}

	if m.ChannelID == a.config.MinecraftDiscordMessengerChannelID {
		if a.minecraftConn == nil {
			log.Printf("Minecraft connection not established cannot forward message to Minecraft mod")
			return
		}

		msg := fmt.Sprintf("[Discord] %s: %s", m.Author.Username, m.Content)

		_, err := fmt.Fprintln(a.minecraftConn, msg)
		if err != nil {
			log.Printf("Error sending to Minecraft mod: %v", err)
		} else {
			log.Printf("Sent to Minecraft: %s", msg)
		}
	}
}
