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
	_ = os.WriteFile("whitelist.json", updatedData, 0644)

	removeWL(removedMCUsername)
	log.Printf("Removed %s from whitelist (Discord ID: %s)", removedMCUsername, discordID)
}