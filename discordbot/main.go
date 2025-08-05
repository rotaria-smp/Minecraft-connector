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

var (
	minecraftConn    net.Conn
	discordSession   *discordgo.Session
	discordChannelID string
)

var latestStatus string = "TPS: ?, Online: ?"

func init() {
	var discordSessionErr error
	var envErr error
	var minecraftTcpErr error

	envErr = godotenv.Load()
	if envErr != nil {
		log.Fatalf("Error loading .env file")
	}

	discordChannelID = os.Getenv("channelid")

	discordSession, discordSessionErr = discordgo.New("Bot " + os.Getenv("dtoken"))
	if discordSessionErr != nil {
		log.Fatalf("Invalid bot parameters: %v", discordSessionErr)
	}

	minecraftConn, minecraftTcpErr = net.Dial("tcp", "localhost:26644")
	if minecraftTcpErr != nil {
		log.Fatalf("Failed to connect to Minecraft mod socket: %v", minecraftTcpErr)
	}

	log.Println("Connected to Minecraft mod socket on localhost:26644")
}

func main() {
	discordSession.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsGuildMembers

	sessionOpenError := discordSession.Open()
	if sessionOpenError != nil {
		log.Fatalf("Cannot open the Discord session: %v", sessionOpenError)
	}

	go readMinecraftMessages()

	discordSession.AddHandler(onDiscordMessage)
	discordSession.AddHandler(onUserLeft)
	discordSession.AddHandler(onWhitelistModalRequested)
	discordSession.AddHandler(onWhitelistModalSubmitted)
	discordSession.AddHandler(onWhitelistModalResponse)

	discordSession.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
		log.Println("Bot is now running. Press CTRL+C to exit.")
	})

	defer discordSession.Close()
	defer minecraftConn.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop

	log.Println("Gracefully shutting down.")
}

func getModalInputValue(i *discordgo.InteractionCreate, customID string) string {
	data := i.ModalSubmitData()
	for _, c := range data.Components {
		if row, ok := c.(*discordgo.ActionsRow); ok {
			for _, ic := range row.Components {
				if input, ok := ic.(*discordgo.TextInput); ok && input.CustomID == customID {
					return input.Value
				}
			}
		}
	}
	return ""
}

func onUserLeft(s *discordgo.Session, m *discordgo.GuildMemberRemove) {
	user := m.User.ID
	log.Println("user left" + user)
	removeFromWhitelistJson(user)
}

func onDiscordMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot || m.ChannelID != discordChannelID {
		return
	}

	msg := fmt.Sprintf("[Discord] %s: %s", m.Author.Username, m.Content)

	_, err := fmt.Fprintln(minecraftConn, msg)
	if err != nil {
		log.Printf("Error sending to Minecraft mod: %v", err)
	} else {
		log.Printf("Sent to Minecraft: %s", msg)
	}

	if m.Content == "!whitelist" {
		sendWhitelistStarter(s, m.ChannelID)
	}
}

func updateBotPresence(status string) {
	err := discordSession.UpdateGameStatus(0, status)
	if err != nil {
		log.Printf("Failed to update bot status: %v", err)
	}
}

func updateVoiceChannelName(channelID, status string) {
	newName := "🟢 " + status
	if len(newName) > 100 {
		newName = newName[:100]
	}

	_, err := discordSession.ChannelEdit(channelID, &discordgo.ChannelEdit{
		Name: newName,
	})
	if err != nil {
		log.Printf("Failed to update voice channel name: %v", err)
	}
}
