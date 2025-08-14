package main

import (
	"fmt"
	"limpan/rotaria-bot/entities"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func (a *App) readMinecraftMessages() {
	if a.MinecraftConn == nil {
		log.Println("Minecraft connection is not established. I will not read messages")
		return
	}

	// Subscribe for events from tcpbridge
	_, events, cancel := a.MinecraftConn.Subscribe(128)
	defer cancel()

	for evt := range events {
		topic := evt.Topic
		body := string(evt.Body)
		log.Printf("Received from Minecraft topic %s: %s", topic, body)
		if body == "" {
			continue
		}

		switch topic {
		case entities.TopicLifecycle:
			fallthrough
		case entities.TopicJoin:
			fallthrough
		case entities.TopicLeave:
			fallthrough
		case entities.TopicChat:
			log.Println("Status update received:", body)
			parts := strings.SplitN(body, " ", 2)
			if len(parts) < 2 {
				log.Printf("Received from Minecraft: %s", body)
				_, err := a.DiscordSession.ChannelMessageSend(a.Config.MinecraftDiscordMessengerChannelID, body)
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
			_, err := a.DiscordSession.ChannelMessageSend(a.Config.MinecraftDiscordMessengerChannelID, cleanedMessage)
			if err != nil {
				log.Printf("Error sending message to Discord: %v", err)
			}
		case entities.TopicStatus:
			log.Println("Chat message received:", body)
			if strings.HasPrefix(body, "[UPDATE]") {
				latestStatus := strings.TrimPrefix(body, "[UPDATE] ")
				log.Println("Status updated:", latestStatus)
				a.updateBotPresence(latestStatus)
				a.setVoiceChannelStatus(a.Config.ServerStatusChannelID, latestStatus)
				continue
			}
		default:
			log.Println("Unknown topic:", topic)
		}
	}
}

func (a *App) updateBotPresence(status string) {
	err := a.DiscordSession.UpdateGameStatus(0, status)
	if err != nil {
		log.Printf("Failed to update bot status: %v", err)
	}
}

func (a *App) setVoiceChannelStatus(channelID, status string) {
	newName := "🟢 " + status
	if len(newName) > 100 {
		newName = newName[:100]
	}

	_, err := a.DiscordSession.ChannelEdit(channelID, &discordgo.ChannelEdit{
		Name: newName,
	})
	if err != nil {
		log.Printf("Failed to update voice channel name: %v", err)
	}
}
