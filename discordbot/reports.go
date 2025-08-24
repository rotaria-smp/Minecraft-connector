package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (a *App) onReportModalSubmitted(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionModalSubmit || i.ModalSubmitData().CustomID != "report_modal" {
		return
	}

	reporter := getSubmittingUser(i)

	reportType := strings.ToLower(strings.TrimSpace(getModalInputValue(i, "report_type")))
	reported := strings.TrimSpace(getModalInputValue(i, "reported_username"))
	reason := strings.TrimSpace(getModalInputValue(i, "report_reason"))
	evidence := strings.TrimSpace(getModalInputValue(i, "report_evidence"))
	context := strings.TrimSpace(getModalInputValue(i, "report_context"))

	// normalize type
	// switch reportType {
	// case "player", "bug", "other":
	// default:
	// 	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
	// 		Type: discordgo.InteractionResponseChannelMessageWithSource,
	// 		Data: &discordgo.InteractionResponseData{
	// 			Content: "❌ Invalid report type. Please enter `player`, `bug`, or `other`.",
	// 			Flags:   discordgo.MessageFlagsEphemeral,
	// 		},
	// 	})
	// 	return
	// }

	// enforce username if player report
	if reportType == "player" && reported == "" {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Please provide the player's username for a Player report.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	a.sendReportForReviewTyped(s, reporter.ID, reportType, reported, reason, evidence, context)

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("✅ Thanks! Your **%s** report has been submitted.", strings.Title(reportType)),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func (a *App) sendReportForReviewTyped(
	s *discordgo.Session,
	reporterID, reportType, reported, reason, evidence, context string,
) {
	title := fmt.Sprintf("New %s Report", strings.Title(reportType))

	fields := []*discordgo.MessageEmbedField{
		{Name: "Reporter", Value: fmt.Sprintf("<@%s>", reporterID), Inline: true},
		{Name: "Type", Value: strings.Title(reportType), Inline: true},
	}

	if reportType == "player" && strings.TrimSpace(reported) != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Reported Player", Value: fmt.Sprintf("`%s`", reported), Inline: true,
		})
	}

	if strings.TrimSpace(reason) != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Details", Value: reason, Inline: false,
		})
	}
	if strings.TrimSpace(evidence) != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Evidence", Value: evidence, Inline: false,
		})
	}
	if strings.TrimSpace(context) != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Context", Value: context, Inline: false,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: "A new report has been filed.",
		Color:       0xF44336, // red-ish
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Rotaria Moderation"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Mark as Resolved",
					Style:    discordgo.SuccessButton,
					CustomID: fmt.Sprintf("report_resolve_%s|%s", reported, reporterID),
				},
				discordgo.Button{
					Label:    "Dismiss",
					Style:    discordgo.DangerButton,
					CustomID: fmt.Sprintf("report_dismiss_%s|%s", reported, reporterID),
				},
			},
		},
	}

	_, err := s.ChannelMessageSendComplex(a.Config.ReportChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
	if err != nil {
		log.Printf("Error sending report embed: %v", err)
	}
}

func (a *App) onReportAction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}
	data := i.MessageComponentData()
	cid := data.CustomID
	if !(strings.HasPrefix(cid, "report_resolve_") || strings.HasPrefix(cid, "report_dismiss_")) {
		return
	}

	action := "resolve"
	if strings.HasPrefix(cid, "report_resolve_") {
		cid = strings.TrimPrefix(cid, "report_resolve_")
	} else {
		cid = strings.TrimPrefix(cid, "report_dismiss_")
		action = "dismiss"
	}

	// Parse "<reported>|<reporter>"
	parts := strings.SplitN(cid, "|", 2)
	reported := ""
	reporter := ""
	if len(parts) == 2 {
		reported, reporter = parts[0], parts[1]
	}

	// We must remember which message to edit after the modal submit:
	channelID := i.ChannelID
	messageID := i.Message.ID

	modalCustomID := fmt.Sprintf("report_action_modal|%s|%s|%s|%s|%s",
		action, channelID, messageID, reported, reporter)

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: modalCustomID,
			Title:    strings.Title(action) + " Report",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "moderator_note",
							Label:       "Add a short note for the record",
							Style:       discordgo.TextInputParagraph,
							Placeholder: "What did you find/decide? Any guidance for the players?",
							Required:    true, // make note required
							MaxLength:   1000,
						},
					},
				},
			},
		},
	})
}

func (a *App) onReportActionModalSubmitted(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionModalSubmit {
		return
	}
	cid := i.ModalSubmitData().CustomID
	if !strings.HasPrefix(cid, "report_action_modal|") {
		return
	}

	// Parse: report_action_modal|<action>|<channelID>|<messageID>|<reported>|<reporter>
	parts := strings.SplitN(cid, "|", 6)
	if len(parts) != 6 {
		return
	}
	action := parts[1] // "resolve" or "dismiss"
	channelID := parts[2]
	messageID := parts[3]
	reported := parts[4]
	reporter := parts[5]

	// ✅ Use your existing helper to fetch the note from the modal
	note := strings.TrimSpace(getModalInputValue(i, "moderator_note"))
	// Optional: log to verify it’s actually present
	log.Printf("Moderator note length: %d", len(note))

	// Fetch original message
	msg, err := s.ChannelMessage(channelID, messageID)
	if err != nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Could not fetch the original message to update.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	color := 0x22C55E
	label := "Resolved"
	if action == "dismiss" {
		color = 0xEF4444
		label = "Dismissed"
	}
	staffer := i.Member.User

	statusLine := fmt.Sprintf(
		"📝 Report on `%s` was **%s** by <@%s>. (Original reporter: <@%s>)",
		reported, label, staffer.ID, reporter,
	)

	var edited *discordgo.MessageEmbed
	if len(msg.Embeds) > 0 {
		cp := *msg.Embeds[0]
		cp.Color = color

		// Append status line to description
		if strings.TrimSpace(cp.Description) == "" {
			cp.Description = statusLine
		} else {
			cp.Description += "\n\n" + statusLine
		}

		// 🔧 Always add/update "Moderator Note"
		found := false
		for _, f := range cp.Fields {
			if strings.EqualFold(f.Name, "Moderator Note") {
				// Update existing field
				if note == "" {
					f.Value = "—"
				} else {
					f.Value = note
				}
				found = true
				break
			}
		}
		if !found {
			value := note
			if value == "" {
				value = "—" // visible placeholder so you know the field was set
			}
			cp.Fields = append(cp.Fields, &discordgo.MessageEmbedField{
				Name:  "Moderator Note",
				Value: value,
			})
		}

		cp.Footer = &discordgo.MessageEmbedFooter{Text: "Rotaria Moderation"}
		cp.Timestamp = time.Now().UTC().Format(time.RFC3339)
		edited = &cp
	} else {
		edited = &discordgo.MessageEmbed{
			Title:       "Report",
			Color:       color,
			Description: statusLine,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Moderator Note", Value: ifEmpty(note, "—")},
			},
			Footer:    &discordgo.MessageEmbedFooter{Text: "Rotaria Moderation"},
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
	}

	// Apply edit + remove buttons
	_, err = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    channelID,
		ID:         messageID,
		Embeds:     &[]*discordgo.MessageEmbed{edited},
		Components: &[]discordgo.MessageComponent{},
	})
	if err != nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ Failed to update the report message.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("✅ %s with note added.", label),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// tiny helper
func ifEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
