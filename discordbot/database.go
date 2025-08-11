package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

type WhiteListEntry struct {
	ID                int
	DiscordID         string
	MinecraftUsername string
}

func (a *App) InitializeDatabase() {
	var err error
	a.DatabaseConn, err = sql.Open("sqlite", a.Config.DatabaseConfigPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	createTable := `CREATE TABLE IF NOT EXISTS whitelist (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		discord_id TEXT NOT NULL,
		minecraft_username TEXT NOT NULL
	);`
	_, err = a.DatabaseConn.Exec(createTable)
	if err != nil {
		log.Fatalf("Failed to create whitelist table: %v", err)
	}
}

func (a *App) CloseDatabase() {
	if a.DatabaseConn != nil {
		a.DatabaseConn.Close()
	}
}

func (a *App) AddWhitelistDatabaseEntry(whitelistEntry WhiteListEntry) error {
	if a.DatabaseConn == nil {
		return sql.ErrConnDone
	}

	alreadyExists, err := a.GetWhitelistEntry(whitelistEntry.DiscordID) // Ensure the entry doesn't already exist
	if err != nil {
		return fmt.Errorf("error checking existing whitelist entry: %v", err)
	}
	if alreadyExists != nil {
		return fmt.Errorf("whitelist entry already exists for Discord ID %s", whitelistEntry.DiscordID)
	}

	_, err = a.DatabaseConn.Exec(`INSERT INTO whitelist (discord_id, minecraft_username) VALUES (?, ?)`, whitelistEntry.DiscordID, whitelistEntry.MinecraftUsername)
	return err
}

func (a *App) RemoveWhitelistDatabaseEntry(id int) error {
	if a.DatabaseConn == nil {
		return sql.ErrConnDone
	}
	_, err := a.DatabaseConn.Exec(`DELETE FROM whitelist WHERE id = ?`, id)
	return err
}

func (a *App) GetWhitelistEntry(discordId string) (*WhiteListEntry, error) {
	if a.DatabaseConn == nil {
		return nil, sql.ErrConnDone
	}
	row := a.DatabaseConn.QueryRow(`SELECT id, discord_id, minecraft_username FROM whitelist WHERE discord_id = ?`, discordId)

	var entry WhiteListEntry
	err := row.Scan(&entry.ID, &entry.DiscordID, &entry.MinecraftUsername)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No entry found
		}
		return nil, err // Other error
	}
	return &entry, nil
}
