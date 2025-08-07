package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)


func initCommands(a *App) []*discordgo.ApplicationCommand {
	commands = []*discordgo.ApplicationCommand{
		{
			Name: "hej",
			Description: "Testing",
		},
		{
			Name: "commands",
			Type: 2, //change to 1 when in prod
			GuildID: "1401855308787093547",
		},
	}

	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			"hej": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: "ehkgjjdhkfsdhjalgjdh",
					},
				})
			},
			"commands": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
				fmt.Println("hejhej")
				cmd := a.Commands
				var comps string
				for _, v := range cmd {
					fmt.Printf("%s", v.Name)
					comps += fmt.Sprintf("- %s\n", v.Name)
				}
				fmt.Println(comps)
				
				s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
					Type: discordgo.InteractionResponseChannelMessageWithSource,
					Data: &discordgo.InteractionResponseData{
						Content: comps,
					},
				})
			},
	}
	return commands
}

func (a *App) handleCommandsCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	fmt.Println("hejhej")
	cmd := a.Commands
	fmt.Printf("commands: %v", cmd)
	var comps []discordgo.MessageComponent
	for _, v := range cmd {
		comps = append(comps, discordgo.TextDisplay{
			Content: v.Name,
		})
	}
	
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Components: comps,
		},
	})
}

func createCommands(s *discordgo.Session, unregisteredCommands []*discordgo.ApplicationCommand) ([]*discordgo.ApplicationCommand, error) {
	var registeredCommands []*discordgo.ApplicationCommand
	for _, v := range unregisteredCommands {
		cmd, err := s.ApplicationCommandCreate(s.State.User.ID, "" , v)
		if err != nil {
			fmt.Println("Something went wrong: ", err)
			return nil, err
		}
		registeredCommands = append(registeredCommands, cmd)
	}
	fmt.Println("Commands have been created")
	return registeredCommands, nil
}

func deleteCommand(s *discordgo.Session, commands []*discordgo.ApplicationCommandInteractionData) {

}