package main

import (
	"fmt"
	"log"
	"strings"

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

func (a *App) sendWLForReview(s *discordgo.Session, mcUsername, discordId, age string) {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	content := fmt.Sprintf("📝 Whitelist request from **<@%s>** for Minecraft username: `%s` and age: %s", discordId, mcUsername, age)

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

	_, err = s.ChannelMessageSendComplex(a.Config.WhitelistRequestsChannelID, &discordgo.MessageSend{
		Content:    content,
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

		a.sendWLForReview(s, minecraftUsername, submittingUser.ID, age)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("✅ Thanks! We'll review your whitelist for `%s` shortly.", minecraftUsername),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})

		// TODO: Remove original modal message

	}
}

func (a *App) onWhitelistModalResponse(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	customID := i.MessageComponentData().CustomID

	if strings.HasPrefix(customID, "approve_") {
		data := strings.TrimPrefix(customID, "approve_")
		parts := strings.SplitN(data, "|", 2)
		if len(parts) != 2 {
			log.Println("Invalid approve_ customID format")
			return
		}
		username := parts[0]
		requester := parts[1]
		a.addWhitelist(requester, username)

		fmt.Fprintf(a.MinecraftConn, "whitelist add %s\n", username)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("✅ Approved `%s` for whitelisting!", username),
				Components: []discordgo.MessageComponent{},
			},
		})
	} else if strings.HasPrefix(customID, "reject_") {
		username := strings.TrimPrefix(customID, "reject_")

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("❌ Rejected `%s` from whitelisting.", username),
				Components: []discordgo.MessageComponent{},
			},
		})
	} else {
		log.Printf("Unknown customID: %s", customID)
	}
}

func (a *App) addWhitelist(discordId, minecraftUsername string) {
	whitelistEntry := WhiteListEntry{
		DiscordID:         discordId,
		MinecraftUsername: minecraftUsername,
	}

	err := a.AddWhitelistDatabaseEntry(whitelistEntry)
	if err != nil {
		log.Printf("Error adding whitelist entry for Discord ID %s: %v", discordId, err)
		return
	}

	if a.MinecraftConn == nil {
		log.Println("Minecraft connection is not established. I will not add the user to the whitelist")
		return
	}

	fmt.Fprintf(a.MinecraftConn, "whitelist add %s\n", minecraftUsername)

	log.Printf("Added %s to whitelist (Discord ID: %s)", minecraftUsername, discordId)
}

func (a *App) removeWhitelist(discordId string) {
	whitelistEntry, err := a.GetWhitelistEntry(discordId)
	if err != nil {
		log.Printf("Error retrieving whitelist entry for Discord ID %s: %v", discordId)
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

	fmt.Fprintf(a.MinecraftConn, "unwhitelist %s\n", whitelistEntry.MinecraftUsername)

	err = a.RemoveWhitelistDatabaseEntry(whitelistEntry.ID)
	if err != nil {
		log.Printf("Error removing whitelist entry for Discord ID %s: %v", discordId, err)
		return
	}

	log.Printf("Removed %s from whitelist (Discord ID: %s)", whitelistEntry.MinecraftUsername, discordId)
}
