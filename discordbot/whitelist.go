package main

import (
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
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

func sendWLForReview(s *discordgo.Session, username, requester, age string) {
	reviewChannelID := "1401895238968021092"

	content := fmt.Sprintf("📝 Whitelist request from **%s** for Minecraft username: `%s` and age: %s", requester, username, age)

	approveID := fmt.Sprintf("approve_%s|%s", username, requester)
	rejectID := fmt.Sprintf("reject_%s|%s", username, requester)

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Approve",
					Style:    discordgo.SuccessButton,
					CustomID: approveID,
				},
				discordgo.Button{
					Label:    "Reject",
					Style:    discordgo.DangerButton,
					CustomID: rejectID,
				},
			},
		},
	}

	_, err := s.ChannelMessageSendComplex(reviewChannelID, &discordgo.MessageSend{
		Content:    content,
		Components: components,
	})
	if err != nil {
		log.Printf("Error sending whitelist review message: %v", err)
	}
}
