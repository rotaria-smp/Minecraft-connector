package main

import (
	"context"
	"fmt"
	"limpan/rotaria-bot/internals/db"
	"limpan/rotaria-bot/internals/tcpbridge"
	"log"
	"os"
	"os/signal"
	"sync/atomic"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

type Config struct {
	DiscordToken                       string
	MinecraftAddress                   string
	WhitelistRequestsChannelID         string
	ReportChannelID                    string
	MinecraftDiscordMessengerChannelID string
	ServerStatusChannelID              string
	DatabaseConfigPath                 string
	MemberRoleID                       string
	GuildID                            string
}

type App struct {
	Config         Config
	DiscordSession *discordgo.Session
	MinecraftConn  *tcpbridge.Client
	Commands       []*discordgo.ApplicationCommand

	// status workers
	statusCh   chan string
	presenceCh chan string

	// internal memory for dedupe/throttle
	lastChannelName atomic.Value // string
	lastChannelEdit atomic.Value // time.Time
	lastPresence    atomic.Value // string
	lastPresenceAt  atomic.Value // time.Time

	// blacklist words
	blacklist []string
}

// TODO: Fuck den här, vi måste lösa det på nått bättre sätt sen
var (
	commandHandlers = make(map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate))
)

func main() {
	app := &App{}

	if err := app.loadConfig(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	bl, err := loadBlacklist("blacklist.txt")
	if err != nil {
		log.Printf("Warning: could not load blacklist.txt: %v", err)
	} else {
		app.blacklist = bl
		log.Printf("Loaded %d blacklisted words", len(bl))
	}

	if err := app.connectToServices(); err != nil {
		log.Fatalf("Failed to connect services: %v", err)
	}

	defer app.shutdown()
	app.setupDiscordHandlers()
	commandsTest, err := app.DiscordSession.ApplicationCommands(app.DiscordSession.State.Application.ID, "")
	if err == nil {
		for _, v := range commandsTest {
			log.Printf("Commands: %v Type: %v", v.Name, v.Type)
		}

	}
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

	a.Config = Config{
		DiscordToken:                       os.Getenv("DiscordToken"),
		MinecraftDiscordMessengerChannelID: os.Getenv("MinecraftDiscordMessengerChannelID"),
		WhitelistRequestsChannelID:         os.Getenv("WhitelistRequestsChannelID"),
		ReportChannelID:                    os.Getenv("ReportChannelID"),
		ServerStatusChannelID:              os.Getenv("ServerStatusChannelID"),
		MinecraftAddress:                   os.Getenv("MinecraftAddress"),
		DatabaseConfigPath:                 os.Getenv("DatabaseConfigPath"),
		MemberRoleID:                       os.Getenv("MemberRoleID"),
		GuildID:                            os.Getenv("GuildID"),
	}

	if a.Config.DiscordToken == "" {
		return fmt.Errorf("missing required environment variables")
	}

	return nil
}

func (a *App) connectToServices() error {
	// Connect to Discord
	var err error
	a.DiscordSession, err = discordgo.New("Bot " + a.Config.DiscordToken)
	if err != nil {
		return fmt.Errorf("invalid bot parameters: %w", err)
	}

	a.DiscordSession.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMembers

	if err := a.DiscordSession.Open(); err != nil {
		return fmt.Errorf("cannot open Discord session: %w", err)
	}

	db.InitializeDatabase(a.Config.DatabaseConfigPath)

	// Connect to Minecraft server
	a.MinecraftConn = tcpbridge.New(a.Config.MinecraftAddress, tcpbridge.Options{}) //, tcpbridge.Options{Log: log.New(os.Stdout, "tcpbridge: ", log.LstdFlags)})
	ctx := context.Background()
	a.MinecraftConn.Start(ctx)
	st := a.MinecraftConn.Status()
	if !st.Connected && st.BreakerState != tcpbridge.BreakerClosed {
		return fmt.Errorf("failed to connect to Minecraft mod socket: %w", tcpbridge.ErrUnavailable)
	}
	log.Printf("Connected to Minecraft mod socket on %s", a.Config.MinecraftAddress)
	return nil
}

func (a *App) setupDiscordHandlers() {
	cmds := initCommands(a)
	a.DiscordSession.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
		log.Println("Bot is now running. Press CTRL+C to exit.")
	})
	var err error
	a.Commands, err = createCommands(a.DiscordSession, cmds)
	if err != nil {
		fmt.Println("Could not create commands")
	}
	//TODO gör funktion som hanterar varje commands handler funktion. Ta in s och i och kalla på handler()
	a.DiscordSession.AddHandler(a.onDiscordMessage)
	a.DiscordSession.AddHandler(a.onWhitelistModalSubmitted)
	a.DiscordSession.AddHandler(a.onWhitelistModalResponse)
	a.DiscordSession.AddHandler(a.onReportModalSubmitted)
	a.DiscordSession.AddHandler(a.onReportAction)
	a.DiscordSession.AddHandler(a.onReportActionModalSubmitted)
	a.DiscordSession.AddHandler(a.onUserLeft)
	a.DiscordSession.AddHandler(onWhitelistModalRequested)
	a.DiscordSession.AddHandler(onApplicationCommand)
}

func (a *App) shutdown() {
	if a.DiscordSession != nil {
		a.DiscordSession.Close()
	}
	if a.MinecraftConn != nil {
		a.MinecraftConn.Close()
	}

	db.Close()
}

func onApplicationCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	cmd, ok := commandHandlers[i.ApplicationCommandData().Name]
	if !ok {
		log.Printf("Unknown command: %s", i.ApplicationCommandData().Name)
		return
	}
	cmd(s, i)
}

func (a *App) onUserLeft(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
	user := m.User.ID
	log.Println("user left" + user)
	a.removeWhitelist(user)
}

func (a *App) onDiscordMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}

	if m.Content == "!whitelist" {
		sendWhitelistStarter(s, m.ChannelID)
	}

	if m.ChannelID == a.Config.MinecraftDiscordMessengerChannelID {
		if a.MinecraftConn == nil {
			log.Printf("Minecraft connection not established cannot forward message to Minecraft mod")
			return
		}

		// Filter Discord → Minecraft with blacklist
		if a.isBlacklisted(m.Content) {
			log.Printf("Blocked blacklisted Discord message: %s", m.Content)
			err := a.DiscordSession.ChannelMessageDelete(m.ChannelID, m.ID)
			if err != nil {
				log.Printf("Failed to delete message: %v", err)
			}
			return
		}

		msg := fmt.Sprintf("[Discord] %s: %s", m.Author.DisplayName(), m.Content)

		ctx := context.Background()
		_, err := a.MinecraftConn.Send(ctx, []byte(msg))
		if err != nil {
			log.Printf("Error sending to Minecraft mod: %v", err)
		} else {
			log.Printf("Sent to Minecraft: %s", msg)
		}
	}
}
