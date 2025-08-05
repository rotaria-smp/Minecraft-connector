package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

var (
	minecraftConn    net.Conn
	discordSession   *discordgo.Session
	discordChannelID string
)

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
	discordSession.AddHandler(onWhiteListModalRequested)
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

func onWhiteListModalRequested(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionMessageComponent && i.MessageComponentData().CustomID == "request_whitelist" {
		showWhitelistModal(s, i)
	}
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

func onWhitelistModalSubmitted(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionModalSubmit && i.ModalSubmitData().CustomID == "whitelist_modal" {
		var submittingUser *discordgo.User

		if i.User != nil {
			submittingUser = i.User
		} else if i.Member != nil {
			submittingUser = i.Member.User
		} else {
			log.Println("Could not determine submitting user")
			return
		}

		minecraftUsername := getModalInputValue(i, "mc_username")
		age := getModalInputValue(i, "age")

		sendWLForReview(s, minecraftUsername, submittingUser.ID, age)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("✅ Thanks! We'll review your whitelist for `%s` shortly.", minecraftUsername),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})

		// TODO: Remove original modal message

	}
}

func onWhitelistModalResponse(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	customID := i.MessageComponentData().CustomID

	if strings.HasPrefix(customID, "approve_") {
		data := strings.TrimPrefix(customID, "approve_")
		parts := strings.SplitN(data, "|", 2)
		if len(parts) != 2 {
			log.Println("Invalid approve_ customID format")
			return
		}
		username := parts[0]
		requester := parts[1]
		saveWLUsername(requester, username)

		fmt.Fprintf(minecraftConn, "whitelist add %s\n", username)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("✅ Approved `%s` for whitelisting!", username),
				Components: []discordgo.MessageComponent{},
			},
		})
	} else if strings.HasPrefix(customID, "reject_") {
		username := strings.TrimPrefix(customID, "reject_")

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("❌ Rejected `%s` from whitelisting.", username),
				Components: []discordgo.MessageComponent{},
			},
		})
	} else {
		log.Printf("Unknown customID: %s", customID)
	}
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

var latestStatus string = "TPS: ?, Online: ?"

func readMinecraftMessages() {
	statusChannelID := os.Getenv("statusChannelID")

	reader := bufio.NewReader(minecraftConn)
	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("Error reading from Minecraft mod: %v", err)
			return
		}
		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}

		if strings.HasPrefix(message, "[UPDATE]") {
			latestStatus = strings.TrimPrefix(message, "[UPDATE] ")
			log.Println("Status updated:", latestStatus)

			updateBotPresence(latestStatus)
			updateVoiceChannelName(statusChannelID, latestStatus) // todo add to env
			continue
		}

		parts := strings.SplitN(message, " ", 2)
		if len(parts) < 2 {
			log.Printf("Received from Minecraft: %s", message)
			_, err = discordSession.ChannelMessageSend(discordChannelID, message)
			if err != nil {
				log.Printf("Error sending message to Discord: %v", err)
			}
			continue
		}

		username := parts[0]
		content := parts[1]
		content = strings.TrimPrefix(content, "literal{")
		content = strings.TrimSuffix(content, "}")
		content = strings.TrimSpace(content)

		cleanedMessage := fmt.Sprintf("%s %s", username, content)

		log.Printf("Received from Minecraft: %s", cleanedMessage)
		_, err = discordSession.ChannelMessageSend(discordChannelID, cleanedMessage)
		if err != nil {
			log.Printf("Error sending message to Discord: %v", err)
		}
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

func removeWL(user any) {
	if minecraftConn == nil {
		log.Println("Minecraft connection is not established. I will not remove the user from the whitelist")
		return
	}

	fmt.Fprintf(minecraftConn, "unwhitelist %s\n", user)
}
