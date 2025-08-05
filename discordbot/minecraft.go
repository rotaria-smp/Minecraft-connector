package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

func readMinecraftMessages() {
	if minecraftConn == nil {
		log.Println("Minecraft connection is not established. I will not read messages")
		return
	}

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
