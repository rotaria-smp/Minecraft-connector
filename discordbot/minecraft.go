package main

import (
	"bufio"
	"fmt"
	"log"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func (a *App) readMinecraftMessages() {
	if a.minecraftConn == nil {
		log.Println("Minecraft connection is not established. I will not read messages")
		return
	}

	reader := bufio.NewReader(a.minecraftConn)
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
			latestStatus := strings.TrimPrefix(message, "[UPDATE] ")
			log.Println("Status updated:", latestStatus)

			a.updateBotPresence(latestStatus)
			a.setVoiceChannelStatus(a.config.ServerStatusChannelID, latestStatus) // todo add to env
			continue
		}

		parts := strings.SplitN(message, " ", 2)
		if len(parts) < 2 {
			log.Printf("Received from Minecraft: %s", message)
			_, err = discordSession.ChannelMessageSend(a.config.MinecraftDiscordMessengerChannelID, message)
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

func (a *App) updateBotPresence(status string) {
	err := a.discordSession.UpdateGameStatus(0, status)
	if err != nil {
		log.Printf("Failed to update bot status: %v", err)
	}
}

func (a *App) setVoiceChannelStatus(channelID, status string) {
	newName := "🟢 " + status
	if len(newName) > 100 {
		newName = newName[:100]
	}

	_, err := a.discordSession.ChannelEdit(channelID, &discordgo.ChannelEdit{
		Name: newName,
	})
	if err != nil {
		log.Printf("Failed to update voice channel name: %v", err)
	}
}
