package main

import (
	"encoding/json"
	"log"
	"os"
)

type WhitelistEntry struct {
	DiscordUsername  string `json:"discord_username"`
	MinecraftUsername string `json:"minecraft_username"`
}

const whitelistFile = "whitelist.json"

func saveWLUsername(discordUsername string, minecraftUsername string) {
	var entries []WhitelistEntry

	// Load existing file
	file, err := os.ReadFile(whitelistFile)
	if err == nil {
		_ = json.Unmarshal(file, &entries)
	}

	// Append new entry
	entries = append(entries, WhitelistEntry{
		DiscordUsername:  discordUsername,
		MinecraftUsername: minecraftUsername,
	})

	// Save back to file
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		log.Printf("Error marshaling JSON: %v", err)
		return
	}

	err = os.WriteFile(whitelistFile, data, 0644)
	if err != nil {
		log.Printf("Error writing whitelist file: %v", err)
	}
}
