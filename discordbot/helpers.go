package main

import (
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

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

func intPtr(v int) *int { return &v }

func extractUsernames(raw string) (full string, username string) {
	// Extract between < and >
	if !strings.HasPrefix(raw, "<") {
		return "", ""
	}
	endIdx := strings.Index(raw, ">")
	if endIdx == -1 {
		return "", ""
	}
	full = raw[1:endIdx]
	// Use regex to get the last word (alphanumeric/underscore) as username
	re := regexp.MustCompile(`([a-zA-Z0-9_]+)$`)
	match := re.FindStringSubmatch(full)
	if len(match) > 1 {
		username = match[1]
	} else {
		username = full
	}
	return full, username
}
