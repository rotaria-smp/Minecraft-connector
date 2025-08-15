package main

import (
	"fmt"
	"limpan/rotaria-bot/entities"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (a *App) readMinecraftMessages() {
	if a.MinecraftConn == nil {
		log.Println("Minecraft connection is not established. I will not read messages")
		return
	}

	// Subscribe to Minecraft events
	_, events, cancel := a.MinecraftConn.Subscribe(10240000)
	defer cancel()

	out := make(chan string, 10240000)
	go func() {
		tokens := make(chan struct{}, 5)
		for i := 0; i < cap(tokens); i++ {
			tokens <- struct{}{}
		}
		refill := time.NewTicker(1 * time.Second)
		defer refill.Stop()

		for {
			select {
			case <-refill.C:
				select {
				case tokens <- struct{}{}:
				default:
				}
			case m, ok := <-out:
				if !ok {
					return
				}
				<-tokens
				if _, err := a.DiscordSession.ChannelMessageSend(a.Config.MinecraftDiscordMessengerChannelID, m); err != nil {
					log.Printf("Error sending message to Discord: %v", err)
				}
			}
		}
	}()

	for {
		evt, ok := <-events
		if !ok {
			close(out)
			return
		}
		body := strings.TrimSpace(string(evt.Body))
		if body == "" {
			continue
		}

		log.Printf("Received from Minecraft: topic=%v body=%q", evt.Topic, body)

		// Update presence/voice
		if evt.Topic == entities.TopicStatus {
			latest := strings.TrimPrefix(body, "[UPDATE] ")
			a.updateBotPresence(latest)
			a.setVoiceChannelStatus(a.Config.ServerStatusChannelID, latest)
			continue
		}

		// Everything else goes to chat
		var msg string
		if evt.Topic == entities.TopicChat {
			username := ""
			content := body
			if strings.HasPrefix(body, "<") {
				if endIdx := strings.Index(body, ">"); endIdx != -1 {
					username = body[:endIdx+1]
					content = strings.TrimSpace(body[endIdx+1:])
				}
			}
			msg = strings.TrimSpace(fmt.Sprintf("%s %s", username, content))
		} else {
			msg = body
		}

		if msg != "" {
			out <- msg
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
