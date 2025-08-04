package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

func main() {
	fmt.Println("Hello, World!")
	err := godotenv.Load()
  if err != nil {
    log.Fatal("Error loading .env file")
  }
	session, _ := discordgo.New("Bot " + os.Getenv("dtoken"))
	session.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages

	err = session.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}

	log.Println("Bot is now running. Press CTRL+C to exit.")

	defer session.Close()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Graceful shutdown")
}