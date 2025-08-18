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
