package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
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

func sendWLForReview(s *discordgo.Session, mcUsername, discordId, age string) {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	whitelistChannelID := os.Getenv("whitelistChannelID")
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

	_, err = s.ChannelMessageSendComplex(whitelistChannelID, &discordgo.MessageSend{
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

func onWhitelistModalSubmitted(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type == discordgo.InteractionModalSubmit && i.ModalSubmitData().CustomID == "whitelist_modal" {
		var submittingUser *discordgo.User

		if i.User != nil {
			submittingUser = i.User
		} else if i.Member != nil {
			submittingUser = i.Member.User
		} else {
			log.Println("Could not determine submitting user")
			return
		}

		minecraftUsername := getModalInputValue(i, "mc_username")
		age := getModalInputValue(i, "age")

		sendWLForReview(s, minecraftUsername, submittingUser.ID, age)

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

func onWhitelistModalResponse(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
		saveWLUsername(requester, username)

		fmt.Fprintf(minecraftConn, "whitelist add %s\n", username)

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

func removeFromWhitelistJson(discordID any) {
	file, err := os.ReadFile(whitelistFile)
	if err != nil {
		log.Println("Error reading whitelist.json:", err)
		return
	}

	var entries []WhitelistEntry
	err = json.Unmarshal(file, &entries)
	if err != nil {
		log.Println("Error parsing JSON:", err)
		return
	}

	var updated []WhitelistEntry
	var removedMCUsername string
	for _, entry := range entries {
		if entry.DiscordUsername == discordID {
			removedMCUsername = entry.MinecraftUsername
			continue
		}
		updated = append(updated, entry)
	}

	if removedMCUsername == "" {
		log.Printf("No whitelist entry found for Discord ID %s", discordID)
		return
	}

	updatedData, _ := json.MarshalIndent(updated, "", "  ")
	if len(updated) == 0 {
		updatedData = []byte("[]") // Ensure we write an empty array if no entries remain
	}
	_ = os.WriteFile("whitelist.json", updatedData, 0644)

	removeWL(removedMCUsername)
	log.Printf("Removed %s from whitelist (Discord ID: %s)", removedMCUsername, discordID)
}

func removeWL(user any) {
	if minecraftConn == nil {
		log.Println("Minecraft connection is not established. I will not remove the user from the whitelist")
		return
	}

	fmt.Fprintf(minecraftConn, "unwhitelist %s\n", user)
}
