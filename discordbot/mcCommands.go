package main

import (
	"context"
	"fmt"
	"limpan/rotaria-bot/entities"
	"limpan/rotaria-bot/internals/db"
	"limpan/rotaria-bot/namemc"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

func sendWhitelistStarter(s *discordgo.Session, channelID string) {
	embed := &discordgo.MessageEmbed{
		Title:       "Get Whitelisted",
		Description: "Click the button below to begin the whitelist process.",
	}

	button := discordgo.Button{
		Label:    "Start Whitelisting",
		Style:    discordgo.PrimaryButton,
		CustomID: "request_whitelist",
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{button},
		},
	}

	_, _ = s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
}

func showWhitelistModal(s *discordgo.Session, i *discordgo.InteractionCreate) {
	modal := &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "whitelist_modal",
			Title:    "Enter Your Minecraft Username",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "mc_username",
							Label:       "Minecraft Username",
							Style:       discordgo.TextInputShort,
							Placeholder: "e.g. Notch",
							Required:    true,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "age",
							Label:       "Whats your age",
							Style:       discordgo.TextInputShort,
							Placeholder: "16+",
							Required:    true,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "info_1",
							Label:       "what do you plan on doing on the server?",
							Style:       discordgo.TextInputShort,
							Placeholder: "build, economy, towns, etc",
							Required:    true,
						},
					},
				},
			},
		},
	}

	s.InteractionRespond(i.Interaction, modal)
}

// add plan parameter
func (a *App) sendWLForReview(s *discordgo.Session, mcUsername, discordId, age, plan string) {
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Whitelist Request",
		Description: "A new whitelist request has been submitted.",
		Color:       0x3B82F6, // blue
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Applicant", Value: fmt.Sprintf("<@%s>", discordId), Inline: true},
			{Name: "Minecraft Username", Value: fmt.Sprintf("`%s`", mcUsername), Inline: true},
			{Name: "Age", Value: age, Inline: true},
			{Name: "Plan on the Server", Value: plan, Inline: false}, // 👈 shows the modal text
		},
		Footer:    &discordgo.MessageEmbedFooter{Text: "Rotaria Whitelist"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Approve",
					Style:    discordgo.SuccessButton,
					CustomID: fmt.Sprintf("approve_%s|%s", mcUsername, discordId),
				},
				discordgo.Button{
					Label:    "Reject",
					Style:    discordgo.DangerButton,
					CustomID: fmt.Sprintf("reject_%s|%s", mcUsername, discordId),
				},
			},
		},
	}

	_, err := s.ChannelMessageSendComplex(a.Config.WhitelistRequestsChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
	if err != nil {
		log.Printf("Error sending whitelist review message: %v", err)
	}
}

func onWhitelistModalRequested(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionMessageComponent && i.MessageComponentData().CustomID == "request_whitelist" {
		showWhitelistModal(s, i)
	}
}

func (a *App) onWhitelistModalSubmitted(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionModalSubmit && i.ModalSubmitData().CustomID == "whitelist_modal" {
		submittingUser := getSubmittingUser(i)

		minecraftUsername := getModalInputValue(i, "mc_username")
		age := getModalInputValue(i, "age")
		plan := getModalInputValue(i, "info_1") // 👈 NEW

		namemcClient := namemc.New()
		uuid, err := namemcClient.UsernameToUUID(minecraftUsername)
		if err != nil {
			log.Printf("Error getting UUID for Minecraft username %s: %v", minecraftUsername, err)
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: fmt.Sprintf("❌ Could not find Minecraft account `%s`. Please ensure the username is spelled correctly", minecraftUsername),
					Flags:   discordgo.MessageFlagsEphemeral,
				},
			})
			return
		}

		log.Printf("UUID for Minecraft username %s: %s", minecraftUsername, uuid)

		// 👇 pass plan to review embed
		a.sendWLForReview(s, minecraftUsername, submittingUser.ID, age, plan)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("✅ Thanks! We'll review your whitelist for `%s` shortly.", minecraftUsername),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	}
}

func (a *App) onWhitelistModalResponse(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	staffID := i.Member.User.ID
	customID := i.MessageComponentData().CustomID

	// helper to update the embed in-place
	updateEmbed := func(approved bool, username, requester string) {
		color := 0x22C55E // green
		label := "approved"
		if !approved {
			color = 0xEF4444 // red
			label = "rejected"
		}

		// clone the first embed so we keep all fields (including the Plan)
		var edited *discordgo.MessageEmbed
		if len(i.Message.Embeds) > 0 {
			cp := *i.Message.Embeds[0]
			cp.Color = color

			// Append a status line to description (keeps Plan field intact)
			status := fmt.Sprintf("📝 Request for `%s` was **%s** by <@%s>. (Requested by: <@%s>)",
				username, strings.Title(label), staffID, requester)
			if strings.TrimSpace(cp.Description) == "" {
				cp.Description = status
			} else {
				cp.Description = cp.Description + "\n\n" + status
			}

			// Optional: add/update a "Decision" field
			found := false
			for _, f := range cp.Fields {
				if strings.EqualFold(f.Name, "Decision") {
					f.Value = strings.Title(label)
					found = true
					break
				}
			}
			if !found {
				cp.Fields = append(cp.Fields, &discordgo.MessageEmbedField{
					Name:   "Decision",
					Value:  strings.Title(label),
					Inline: false,
				})
			}

			cp.Footer = &discordgo.MessageEmbedFooter{Text: "Rotaria Whitelist"}
			cp.Timestamp = time.Now().UTC().Format(time.RFC3339)
			edited = &cp
		} else {
			// Fallback (shouldn't happen in this flow)
			edited = &discordgo.MessageEmbed{
				Title:       "Whitelist Request",
				Description: fmt.Sprintf("Request for `%s` was %s by <@%s>.", username, label, staffID),
				Color:       color,
			}
		}

		// Update original message: replace embed, remove buttons
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{edited},
				Components: []discordgo.MessageComponent{},
			},
		})
	}

	switch {
	case strings.HasPrefix(customID, "approve_"):
		data := strings.TrimPrefix(customID, "approve_")
		parts := strings.SplitN(data, "|", 2)
		if len(parts) != 2 {
			log.Println("Invalid approve_ customID format")
			return
		}
		username := parts[0]
		requester := parts[1]

		a.addWhitelist(requester, username)

		// tell the MC mod (already in your code)
		ctx := context.Background()
		if a.MinecraftConn != nil {
			if _, err := a.MinecraftConn.Send(ctx, []byte(fmt.Sprintf("whitelist add %s\n", username))); err != nil {
				log.Printf("Error sending to Minecraft mod: %v", err)
			}
		}

		// role assignment (already in your code)
		if err := s.GuildMemberRoleAdd(a.Config.GuildID, requester, a.Config.MemberRoleID); err != nil {
			log.Printf("Failed to assign role to %s: %v", requester, err)
		}

		// ✅ EDIT the embed, keeping the Plan field visible
		updateEmbed(true, username, requester)

		// Optional: DM the user (include their plan if you want)
		if dm, err := s.UserChannelCreate(requester); err == nil {
			_, _ = s.ChannelMessageSend(dm.ID, fmt.Sprintf(
				"✅ You have been whitelisted on Rotaria!\nWelcome, `%s` 🎉",
				username,
			))
		}

	case strings.HasPrefix(customID, "reject_"):
		data := strings.TrimPrefix(customID, "reject_")
		parts := strings.SplitN(data, "|", 2)
		if len(parts) != 2 {
			log.Println("Invalid reject_ customID format")
			return
		}
		username := parts[0]
		requester := parts[1]

		updateEmbed(false, username, requester)

	default:
		log.Printf("Unknown customID: %s", customID)
	}
}

func (a *App) addWhitelist(discordId, minecraftUsername string) {
	whitelistEntry := entities.WhiteListEntry{
		DiscordID:         discordId,
		MinecraftUsername: minecraftUsername,
	}

	err := db.AddWhitelistDatabaseEntry(whitelistEntry)
	if err != nil {
		log.Printf("Error adding whitelist entry for Discord ID %s: %v", discordId, err)
		return
	}

	if a.MinecraftConn == nil {
		log.Println("Minecraft connection is not established. I will not add the user to the whitelist")
		return
	}

	msg := fmt.Sprintf("whitelist add %s\n", minecraftUsername)
	ctx := context.Background()
	_, err = a.MinecraftConn.Send(ctx, []byte(msg))
	if err != nil {
		log.Printf("Error sending to Minecraft mod: %v", err)
	}

	log.Printf("Added %s to whitelist (Discord ID: %s)", minecraftUsername, discordId)
}

func (a *App) removeWhitelist(discordId string) {
	whitelistEntry, err := db.GetWhitelistEntry(discordId)
	if err != nil {
		log.Printf("Error retrieving whitelist entry for Discord ID %s: %v", discordId, err)
		return
	}
	if whitelistEntry == nil {
		log.Printf("No whitelist entry found for Discord ID %s", discordId)
		return
	}

	if a.MinecraftConn == nil {
		log.Println("Minecraft connection is not established. I will not remove the user from the whitelist")
		return
	}

	msg := fmt.Sprintf("unwhitelist %s\n", whitelistEntry.MinecraftUsername)
	ctx := context.Background()
	_, err = a.MinecraftConn.Send(ctx, []byte(msg))
	if err != nil {
		log.Printf("Error sending to Minecraft mod: %v", err)
	}

	err = db.RemoveWhitelistDatabaseEntry(whitelistEntry.ID)
	if err != nil {
		log.Printf("Error removing whitelist entry for Discord ID %s: %v", discordId, err)
		return
	}

	log.Printf("Removed %s from whitelist (Discord ID: %s)", whitelistEntry.MinecraftUsername, discordId)
}

func (a *App) executeNonPrivilagedCommand(s *discordgo.Session, i *discordgo.InteractionCreate, command string) string {
	if a.MinecraftConn == nil {
		log.Println("Minecraft connection is not established. Cannot execute command")
		return ""
	}
	msg := fmt.Sprintf("commandexec %s\n", command)
	ctx := context.Background()
	response, err := a.MinecraftConn.Send(ctx, []byte(msg))
	if err != nil {
		log.Printf("Error sending command to Minecraft mod: %v", err)
		return ""
	}

	log.Printf("Sent command to Minecraft: %s", command)
	return string(response)
}

func (a *App) kickPlayer(minecraftUsername string) {
	if a.MinecraftConn == nil {
		log.Println("Minecraft connection is not established. Cannot kick the player")
		return
	}

	msg := fmt.Sprintf("kick %s\n", minecraftUsername)
	ctx := context.Background()
	_, err := a.MinecraftConn.Send(ctx, []byte(msg))
	if err != nil {
		log.Printf("Error sending kick command to Minecraft mod: %v", err)
		return
	}

	log.Printf("Sent kick command for player %s (Discord ID: %s)", minecraftUsername)
}
