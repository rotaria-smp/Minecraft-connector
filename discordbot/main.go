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

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	discordToken := os.Getenv("dtoken")
	discordChannelID = os.Getenv("channelid")

	minecraftConn, err = net.Dial("tcp", "localhost:26644")
	if err != nil {
		log.Fatalf("Failed to connect to Minecraft mod socket: %v", err)
	}
	log.Println("Connected to Minecraft mod socket on localhost:26644")
	defer minecraftConn.Close()

	discordSession, err = discordgo.New("Bot " + discordToken)
	if err != nil {
		log.Fatalf("Failed to create Discord session: %v", err)
	}
	discordSession.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages
	discordSession.AddHandler(onDiscordMessage)

	err = discordSession.Open()
	if err != nil {
		log.Fatalf("Cannot open the Discord session: %v", err)
	}
	defer discordSession.Close()

	log.Println("Bot is now running. Press CTRL+C to exit.")

	go readMinecraftMessages()

	discordSession.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
    if i.Type == discordgo.InteractionMessageComponent && i.MessageComponentData().CustomID == "request_whitelist" {
        showWhitelistModal(s, i)
    }
	})
		
	discordSession.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
    if i.Type == discordgo.InteractionModalSubmit && i.ModalSubmitData().CustomID == "whitelist_modal" {
        username := i.ModalSubmitData().Components[0].(*discordgo.ActionsRow).Components[0].(*discordgo.TextInput).Value

		sendWLForReview(s, username, i.Member.User.Username)

        s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
            Type: discordgo.InteractionResponseChannelMessageWithSource,
            Data: &discordgo.InteractionResponseData{
                Content: fmt.Sprintf("✅ Thanks! We'll review your whitelist for `%s` shortly.", username),
                Flags:   discordgo.MessageFlagsEphemeral,
            },
        })
    }
	discordSession.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	customID := i.MessageComponentData().CustomID
if strings.HasPrefix(customID, "approve_") {
	username := strings.TrimPrefix(customID, "approve_")

	fmt.Fprintf(minecraftConn, "whitelist %s\n", username)

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    fmt.Sprintf("✅ Approved `%s` for whitelisting!", username),
			Components: []discordgo.MessageComponent{},
		},
	})
}else if strings.HasPrefix(customID, "reject_") {
		username := strings.TrimPrefix(customID, "reject_")

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("❌ Rejected `%s` from whitelisting.", username),
				Components: []discordgo.MessageComponent{},
			},
		})
	}
})

})
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Graceful shutdown")
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

func readMinecraftMessages() {
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
