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

	customID := i.MessageComponentData().CustomID
	if !(strings.HasPrefix(customID, "report_resolve_") || strings.HasPrefix(customID, "report_dismiss_")) {
		return
	}

	// Parse CustomID format: "report_resolve_<reported>|<reporter>"
	action := "resolved"
	color := 0x22C55E // green
	if strings.HasPrefix(customID, "report_resolve_") {
		customID = strings.TrimPrefix(customID, "report_resolve_")
	} else {
		customID = strings.TrimPrefix(customID, "report_dismiss_")
		action = "dismissed"
		color = 0xEF4444 // red
	}

	parts := strings.SplitN(customID, "|", 2)
	reported := ""
	reporter := ""
	if len(parts) == 2 {
		reported = parts[0]
		reporter = parts[1]
	}

	resolverID := i.Member.User.ID
	now := time.Now().UTC().Format(time.RFC3339)

	// Safely copy the first embed (the one we sent)
	var newEmbed *discordgo.MessageEmbed
	if len(i.Message.Embeds) > 0 {
		orig := i.Message.Embeds[0]
		// shallow copy is fine for our usage
		cp := *orig
		cp.Color = color

		// Append/replace a "Status" field
		statusValue := fmt.Sprintf("**%s** by <@%s> at %s", strings.Title(action), resolverID, time.Now().Format("2006-01-02 15:04 MST"))
		// Try to find existing Status field to replace
		updated := false
		for _, f := range cp.Fields {
			if strings.EqualFold(f.Name, "Status") {
				f.Value = statusValue
				updated = true
				break
			}
		}
		if !updated {
			cp.Fields = append(cp.Fields, &discordgo.MessageEmbedField{
				Name:  "Status",
				Value: statusValue,
			})
		}

		// Optional: update footer/timestamp to reflect action time
		cp.Footer = &discordgo.MessageEmbedFooter{Text: "Rotaria Moderation"}
		cp.Timestamp = now

		newEmbed = &cp
	} else {
		// Fallback: build a simple embed if original missing
		newEmbed = &discordgo.MessageEmbed{
			Title:       "Report",
			Description: "Status updated.",
			Color:       color,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Status", Value: fmt.Sprintf("**%s** by <@%s>", strings.Title(action), resolverID)},
			},
			Timestamp: now,
		}
	}

	// Remove buttons by sending an Update response with empty Components
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Embeds:     []*discordgo.MessageEmbed{newEmbed},
			Components: []discordgo.MessageComponent{}, // removes buttons
		},
	})
	if err != nil {
		log.Printf("Failed to update report message: %v", err)
		return
	}

	// Post a follow-up message beneath the embed (normal channel send)
	msgTxt := ""
	if reported != "" {
		msgTxt = fmt.Sprintf("📝 Report on `%s` was **%s** by <@%s>.", reported, strings.Title(action), resolverID)
	} else {
		msgTxt = fmt.Sprintf("📝 Report was **%s** by <@%s>.", strings.Title(action), resolverID)
	}
	if reporter != "" {
		msgTxt += fmt.Sprintf(" (Original reporter: <@%s>)", reporter)
	}

	if _, err := s.ChannelMessageSend(i.ChannelID, msgTxt); err != nil {
		log.Printf("Failed to send follow-up status message: %v", err)
	}

	// Optional: DM the original reporter with outcome
	if reporter != "" {
		if dm, derr := s.UserChannelCreate(reporter); derr == nil {
			_, _ = s.ChannelMessageSend(dm.ID, fmt.Sprintf(
				"Hi! Your report%s has been %s by our staff. Thanks for helping keep Rotaria safe!",
				func() string {
					if reported == "" {
						return ""
					}
					return " on `" + reported + "`"
				}(),
				strings.Title(action),
			))
		}
	}
}
