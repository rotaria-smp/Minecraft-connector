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
	// Throw away a job and
	a.startStatusWorkers()

	// Subscribe to Minecraft events
	_, events, cancel := a.MinecraftConn.Subscribe(4096)
	defer cancel()

	// Chat sender (unchanged, still ~1 msg/sec)
	out := make(chan string, 2048)
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

		// Log for visibility (optional)
		log.Printf("Received from Minecraft: topic=%v body=%q", evt.Topic, body)

		if evt.Topic == entities.TopicStatus {
			latest := strings.TrimPrefix(body, "[UPDATE] ")

			// push to workers
			select {
			case a.presenceCh <- latest:
			default:
			}
			select {
			case a.statusCh <- latest:
			default:
			}

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
			// don’t block the loop if out is momentarily full
			select {
			case out <- msg:
			default:
				// if you prefer dropping to avoid head-of-line blocking:
				log.Printf("chat queue full; dropping message")
			}
		}
	}
}

func (a *App) startStatusWorkers() {
	if a.statusCh == nil {
		a.statusCh = make(chan string, 64)
	}
	if a.presenceCh == nil {
		a.presenceCh = make(chan string, 64)
	}

	// sensible defaults
	a.lastChannelEdit.Store(time.Time{})
	a.lastPresenceAt.Store(time.Time{})

	// Worker: channel rename (hard-throttle to 1 per 10 minutes)
	go func() {
		const minRenameGap = 10 * time.Minute
		for status := range a.statusCh {
			desired := "🟢 " + status
			if len(desired) > 100 {
				desired = desired[:100]
			}

			lastName, _ := a.lastChannelName.Load().(string)
			if desired == lastName {
				continue // no-op: same name
			}

			lastEdit, _ := a.lastChannelEdit.Load().(time.Time)
			if since := time.Since(lastEdit); since < minRenameGap {
				// coalesce: skip until window opens; keep last desired in memory
				// You could implement a timer to apply the latest pending name once the window opens.
				continue
			}

			// perform edit (best-effort)
			_, err := a.DiscordSession.ChannelEdit(a.Config.ServerStatusChannelID, &discordgo.ChannelEdit{
				Name: desired,
			})
			if err != nil {
				log.Printf("Failed to update voice channel name: %v", err)
				// On error, don't update lastChannelEdit; we’ll try again when next status arrives and window allows.
				continue
			}
			a.lastChannelName.Store(desired)
			a.lastChannelEdit.Store(time.Now())
		}
	}()

	// Worker: presence update (soft-throttle to 20s)
	go func() {
		const minPresenceGap = 20 * time.Second
		for status := range a.presenceCh {
			last, _ := a.lastPresence.Load().(string)
			lastAt, _ := a.lastPresenceAt.Load().(time.Time)
			if status == last && time.Since(lastAt) < minPresenceGap {
				continue
			}
			if time.Since(lastAt) < minPresenceGap {
				// too soon; skip duplicate/near-duplicate updates
				continue
			}
			if err := a.DiscordSession.UpdateGameStatus(0, status); err != nil {
				log.Printf("Failed to update bot status: %v", err)
				// Don’t bump lastPresenceAt to allow a retry on next event.
				continue
			}
			a.lastPresence.Store(status)
			a.lastPresenceAt.Store(time.Now())
		}
	}()
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
