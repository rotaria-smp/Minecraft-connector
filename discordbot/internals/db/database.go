package db

import (
	"database/sql"
	"fmt"
	"limpan/rotaria-bot/entities"
	"log"

	_ "modernc.org/sqlite"
)

type DbClient struct {
	Conn *sql.DB
}

var db DbClient

func InitializeDatabase(databaseConfigPath string) DbClient {
	var err error
	db.Conn, err = sql.Open("sqlite", databaseConfigPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	createTable := `CREATE TABLE IF NOT EXISTS whitelist (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		discord_id TEXT NOT NULL,
		minecraft_username TEXT NOT NULL
	);`
	_, err = db.Conn.Exec(createTable)
	if err != nil {
		log.Fatalf("Failed to create whitelist table: %v", err)
	}

	return db
}

func DatabaseStatus() {
	if db.Conn == nil {
		log.Println("Database connection is not established.")
		return
	}

	var count int
	err := db.Conn.QueryRow(`SELECT COUNT(*) FROM whitelist`).Scan(&count)
	if err != nil {
		log.Printf("Error checking database status: %v", err)
		return
	}

	log.Printf("Database status: %d entries in whitelist", count)
}

func Close() {
	if db.Conn != nil {
		db.Conn.Close()
	}
}

func AddWhitelistDatabaseEntry(whitelistEntry entities.WhiteListEntry) error {
	if db.Conn == nil {
		return sql.ErrConnDone
	}

	alreadyExists, err := GetWhitelistEntry(whitelistEntry.DiscordID) // Ensure the entry doesn't already exist
	if err != nil {
		return fmt.Errorf("error checking existing whitelist entry: %v", err)
	}
	if alreadyExists != nil {
		return fmt.Errorf("whitelist entry already exists for Discord ID %s", whitelistEntry.DiscordID)
	}

	_, err = db.Conn.Exec(`INSERT INTO whitelist (discord_id, minecraft_username) VALUES (?, ?)`, whitelistEntry.DiscordID, whitelistEntry.MinecraftUsername)
	return err
}

func RemoveWhitelistDatabaseEntry(id int) error {
	if db.Conn == nil {
		return sql.ErrConnDone
	}
	_, err := db.Conn.Exec(`DELETE FROM whitelist WHERE id = ?`, id)
	return err
}

func GetWhitelistEntry(discordId string) (*entities.WhiteListEntry, error) {
	if db.Conn == nil {
		return nil, sql.ErrConnDone
	}
	row := db.Conn.QueryRow(`SELECT id, discord_id, minecraft_username FROM whitelist WHERE discord_id = ?`, discordId)

	var entry entities.WhiteListEntry
	err := row.Scan(&entry.ID, &entry.DiscordID, &entry.MinecraftUsername)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No entry found
		}
		return nil, err // Other error
	}
	return &entry, nil
}
