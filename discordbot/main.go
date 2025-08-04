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
