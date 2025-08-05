package main

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

func generateStatusString(status MinecraftServerStatus) string {
	return fmt.Sprintf("TPS: %d, Online: %d", status.TPS, status.PlayerCount)
}

func getModalInputValue(i *discordgo.InteractionCreate, customID string) string {
	data := i.ModalSubmitData()
	for _, c := range data.Components {
		if row, ok := c.(*discordgo.ActionsRow); ok {
			for _, ic := range row.Components {
				if input, ok := ic.(*discordgo.TextInput); ok && input.CustomID == customID {
					return input.Value
				}
			}
		}
	}
	return ""
}

func getSubmittingUser(i *discordgo.InteractionCreate) *discordgo.User {
	if i.User != nil {
		return i.User
	}
	if i.Member != nil {
		return i.Member.User
	}
	return nil
}
