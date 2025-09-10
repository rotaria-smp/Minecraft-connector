package main

import (
	"fmt"
	"limpan/rotaria-bot/entities"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/discordwebhook"
)

func loadBlacklist(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var blacklist []string
	for _, l := range lines {
		word := strings.TrimSpace(l)
		if word != "" {
			blacklist = append(blacklist, word)
		}
	}
	return blacklist, nil
}

func (a *App) isBlacklisted(msg string) bool {
	lower := strings.ToLower(msg)
	for _, w := range a.blacklist {
		if strings.Contains(lower, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

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
	out := make(chan discordwebhook.Message, 4096)
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
			case msg, ok := <-out:
				if !ok {
					return
				}
				<-tokens
				if err := discordwebhook.SendMessage(a.Config.MessageWebhookUrl, msg); err != nil {
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
		username := ""
		content := body

		if evt.Topic == entities.TopicChat && strings.HasPrefix(body, "<") {
			if endIdx := strings.Index(body, ">"); endIdx != -1 {
				username = body[1:endIdx] // cleaned Minecraft username
				content = strings.TrimSpace(body[endIdx+1:])
			}
		}

		msg = content // only the message content for blacklist checking

		if msg != "" && a.isBlacklisted(msg) {
			log.Printf("Blocked blacklisted message from %s: %q", username, msg)
			a.kickPlayer(username)
			continue
		}

		avatar := fmt.Sprintf("https://minotar.net/avatar/%s/128.png", username)
		flag := discordwebhook.MessageFlagSuppressNotifications

		genericEventUsername := "Rotaria"
		rotariaAvatar := "https://cdn.discordapp.com/icons/1373389493218050150/24f94fe60c73b4af4956f10dbecb5919.webp"

		message := discordwebhook.Message{
			Content:   &msg,
			Username:  &username,
			AvatarURL: &avatar,
			Flags:     &flag,
		}
		switch evt.Topic {
		case entities.TopicJoin, entities.TopicLeave, entities.TopicLifecycle:
			message.Username = &genericEventUsername
			message.AvatarURL = &rotariaAvatar
		}

		if strings.Contains(*message.Content, "@") {
			log.Printf("Blocked blacklisted message from %s: %q", username, *message.Content)

			*message.Content = ""
		}

		if *message.Content != "" {
			select {
			case out <- message:
			default:
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
