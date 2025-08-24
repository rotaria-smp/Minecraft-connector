package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

func initCommands(a *App) []*discordgo.ApplicationCommand {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "commands",
			Description: "Show a list of available commands",
		},
		{
			Name:        "whitelist",
			Description: "Begin whitelist application",
		},
		{
			Name:        "list",
			Description: "List all current online players",
		},
		{
			Name:        "report",
			Description: "Report an issue on the server",
		},
	}

	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"commands": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			cmd := a.Commands
			var comps string
			for _, v := range cmd {
				fmt.Printf("%s", v.Name)
				comps += fmt.Sprintf("\n- %s\n", v.Name)
			}

			embed := discordgo.MessageEmbed{
				Title:       "Commands",
				Description: comps,
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{&embed},
					Flags:  discordgo.MessageFlagsEphemeral,
				},
			})
		},
		"whitelist": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			showWhitelistModal(s, i)
		},
		"list": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			serverResponse := a.executeNonPrivilagedCommand(s, i, "list")

			embed := discordgo.MessageEmbed{
				Title:       "Commands",
				Description: serverResponse,
			}
			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Embeds: []*discordgo.MessageEmbed{&embed},
					Flags:  discordgo.MessageFlagsEphemeral,
				},
			})
		},
		"report": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseModal,
				Data: &discordgo.InteractionResponseData{
					CustomID: "report_modal",
					Title:    "Report an Issue",
					Components: []discordgo.MessageComponent{
						// Report Type (player, bug, other)
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.TextInput{
									CustomID:    "report_type",
									Label:       "Report Type (player, bug, other)",
									Style:       discordgo.TextInputShort,
									Placeholder: "player | bug | other",
									Required:    true,
									MaxLength:   16,
								},
							},
						},
						// Reported player (optional unless type=player)
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.TextInput{
									CustomID:    "reported_username",
									Label:       "Player username (only if type = player)",
									Style:       discordgo.TextInputShort,
									Placeholder: "e.g. Griefer123",
									Required:    false,
									MaxLength:   64,
								},
							},
						},
						// Reason / details
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.TextInput{
									CustomID:    "report_reason",
									Label:       "What happened?",
									Style:       discordgo.TextInputParagraph,
									Placeholder: "Describe the incident, bug, or issue.",
									Required:    true,
									MaxLength:   1000,
								},
							},
						},
						// Evidence links (optional)
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.TextInput{
									CustomID:    "report_evidence",
									Label:       "Evidence (links, optional)",
									Style:       discordgo.TextInputShort,
									Placeholder: "e.g. screenshot/video links",
									Required:    false,
									MaxLength:   200,
								},
							},
						},
						// Context (optional)
						discordgo.ActionsRow{
							Components: []discordgo.MessageComponent{
								discordgo.TextInput{
									CustomID:    "report_context",
									Label:       "Where/when? (optional)",
									Style:       discordgo.TextInputShort,
									Placeholder: "Area, coordinates, time, etc.",
									Required:    false,
									MaxLength:   200,
								},
							},
						},
					},
				},
			})
		},
	}
	return commands
}

func createCommands(s *discordgo.Session, unregisteredCommands []*discordgo.ApplicationCommand) ([]*discordgo.ApplicationCommand, error) {
	var registeredCommands []*discordgo.ApplicationCommand
	for _, v := range unregisteredCommands {
		cmd, err := s.ApplicationCommandCreate(s.State.User.ID, "", v)
		if err != nil {
			fmt.Println("Something went wrong: ", err)
			return nil, err
		}
		registeredCommands = append(registeredCommands, cmd)
	}
	fmt.Println("Commands have been created")
	return registeredCommands, nil
}

func deleteCommand(s *discordgo.Session, commands []*discordgo.ApplicationCommand, app *App, toDelete string) {
	err := s.ApplicationCommandDelete(app.DiscordSession.State.Application.ID, app.DiscordSession.State.Application.GuildID, toDelete)
	if err != nil {
		fmt.Println("Could not delete commands")
	}
}
