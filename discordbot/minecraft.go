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

	msgChan := make(chan string, 200)

	go func() {
		for msg := range msgChan {
			go func(m string) {
				if _, err := a.DiscordSession.ChannelMessageSend(a.Config.MinecraftDiscordMessengerChannelID, m); err != nil {
					log.Printf("Error sending message to Discord: %v", err)
				}
			}(msg)
		}
	}()

	_, events, cancel := a.MinecraftConn.Subscribe(8192)
	defer cancel()

	for evt := range events {
		topic := evt.Topic
		body := string(evt.Body)
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
			username := ""
			content := body
			if strings.HasPrefix(body, "<") {
				if endIdx := strings.Index(body, ">"); endIdx != -1 {
					username = body[:endIdx+1]
					content = strings.TrimSpace(body[endIdx+1:])
				}
			}

			fullMessage := fmt.Sprintf("%s %s", username, content)
			log.Println("Chat message received:", fullMessage)
			msgChan <- fullMessage

		case entities.TopicStatus:
			log.Println("Topic status received:", body)
			if strings.HasPrefix(body, "[UPDATE]") {
				latestStatus := strings.TrimPrefix(body, "[UPDATE] ")
				log.Println("Status updated:", latestStatus)
				a.updateBotPresence(latestStatus)
				a.setVoiceChannelStatus(a.Config.ServerStatusChannelID, latestStatus)
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
