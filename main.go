package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
)

var (
	// Regex to match custom Discord emojis: <:name:id> or <a:name:id>
	customEmojiRegex = regexp.MustCompile(`<a?:(\w+):(\d+)>`)
	db               *sql.DB
	botState         *state.State
)

// Database schema
const schema = `
CREATE TABLE IF NOT EXISTS emojis (
	server_id BIGINT,
	emote_id BIGINT,
	emote_name TEXT NOT NULL,
	usage_count INTEGER DEFAULT 1,
	first_used DATETIME DEFAULT CURRENT_TIMESTAMP,
	last_used DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(server_id, emote_id)
);

CREATE TABLE IF NOT EXISTS stickers (
	server_id BIGINT,
	sticker_id BIGINT,
	sticker_name TEXT NOT NULL,
	usage_count INTEGER DEFAULT 1,
	first_used DATETIME DEFAULT CURRENT_TIMESTAMP,
	last_used DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(server_id, sticker_id)
);

CREATE INDEX IF NOT EXISTS idx_emojis_server_id_emote_id_usage_count ON emojis(server_id, emote_id, usage_count);

CREATE INDEX IF NOT EXISTS idx_stickers_server_id_sticker_id_usage_count ON stickers(server_id, sticker_id, usage_count);
`

func initDB() error {
	var err error
	db, err = sql.Open("sqlite3", "./emote_tracker.db")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	log.Println("Database initialized successfully")
	return nil
}

// Track custom emoji usage
func trackCustomEmoji(emojiName string, emojiID int64, serverID int64) error {
	query := `
		INSERT INTO emojis (server_id, emote_id, emote_name, usage_count)
		VALUES (?, ?, ?, 1)
		ON CONFLICT(server_id, emote_id) DO UPDATE SET
			usage_count = usage_count + 1,
			last_used = CURRENT_TIMESTAMP
	`
	_, err := db.Exec(query, serverID, emojiID, emojiName)
	if err != nil {
		return fmt.Errorf("failed to track custom emoji: %w", err)
	}
	return nil
}

// Decrease custom emoji usage count
func decreaseCustomEmoji(emojiID int64, serverID int64) error {
	query := `
		UPDATE emojis 
		SET usage_count = CASE 
			WHEN usage_count > 0 THEN usage_count - 1 
			ELSE 0 
		END,
		last_used = CURRENT_TIMESTAMP
		WHERE server_id = ? AND emote_id = ?
	`
	_, err := db.Exec(query, serverID, emojiID)
	if err != nil {
		return fmt.Errorf("failed to decrease custom emoji count: %w", err)
	}
	return nil
}

// Track sticker usage
func trackSticker(stickerID int64, stickerName string, serverID int64) error {
	query := `
		INSERT INTO stickers (server_id, sticker_id, sticker_name, usage_count)
		VALUES (?, ?, ?, 1)
		ON CONFLICT(server_id, sticker_id) DO UPDATE SET
			usage_count = usage_count + 1,
			last_used = CURRENT_TIMESTAMP
	`
	_, err := db.Exec(query, serverID, stickerID, stickerName)
	if err != nil {
		return fmt.Errorf("failed to track sticker: %w", err)
	}
	return nil
}

// Extract and track custom emojis from text
func processCustomEmojis(content string, serverID int64) {
	matches := customEmojiRegex.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) == 3 {
			emojiName := match[1]
			emojiIDStr := match[2]

			emojiID, err := strconv.ParseInt(emojiIDStr, 10, 64)
			if err != nil {
				log.Printf("Error parsing emoji ID %s: %v", emojiIDStr, err)
				continue
			}

			if err := trackCustomEmoji(emojiName, emojiID, serverID); err != nil {
				log.Printf("Error tracking custom emoji %s: %v", emojiName, err)
			} else {
				log.Printf("Tracked custom emoji: %s (ID: %d)", emojiName, emojiID)
			}
		}
	}
}

// Process stickers from a message
func processStickers(stickers []discord.StickerItem, serverID int64) {
	for _, sticker := range stickers {
		stickerID := int64(sticker.ID)
		stickerName := sticker.Name

		if err := trackSticker(stickerID, stickerName, serverID); err != nil {
			log.Printf("Error tracking sticker %s: %v", stickerName, err)
		} else {
			log.Printf("Tracked sticker: %s (ID: %d)", stickerName, stickerID)
		}
	}
}

// Handle message creation events
func handleMessageCreate(m *gateway.MessageCreateEvent) {
	// Skip bot messages
	if m.Author.Bot {
		return
	}

	// Only track messages from guilds
	if !m.GuildID.IsValid() {
		return
	}

	serverID := int64(m.GuildID)

	// Process custom emojis
	processCustomEmojis(m.Content, serverID)

	// Process stickers
	if len(m.Stickers) > 0 {
		processStickers(m.Stickers, serverID)
	}
}

// Handle reaction add events
func handleMessageReactionAdd(r *gateway.MessageReactionAddEvent) {
	// Only track reactions in guilds
	if !r.GuildID.IsValid() {
		return
	}

	// Only track custom emojis
	if !r.Emoji.IsCustom() {
		return
	}

	serverID := int64(r.GuildID)
	emojiID := int64(r.Emoji.ID)
	emojiName := r.Emoji.Name

	if err := trackCustomEmoji(emojiName, emojiID, serverID); err != nil {
		log.Printf("Error tracking reaction emoji %s: %v", emojiName, err)
	} else {
		log.Printf("Tracked reaction emoji: %s (ID: %d)", emojiName, emojiID)
	}
}

// Handle reaction remove events
func handleMessageReactionRemove(r *gateway.MessageReactionRemoveEvent) {
	// Only track reactions in guilds
	if !r.GuildID.IsValid() {
		return
	}

	// Only track custom emojis
	if !r.Emoji.IsCustom() {
		return
	}

	serverID := int64(r.GuildID)
	emojiID := int64(r.Emoji.ID)

	if err := decreaseCustomEmoji(emojiID, serverID); err != nil {
		log.Printf("Error decreasing reaction emoji count (ID: %d): %v", emojiID, err)
	} else {
		log.Printf("Decreased reaction emoji count (ID: %d)", emojiID)
	}
}

// Emoji data for pagination
type EmojiData struct {
	Name  string
	ID    int64
	Count int
}

// Sticker data for pagination
type StickerData struct {
	Name  string
	ID    int64
	Count int
}

// Check if user is in a guild (permission check is done by Discord via DefaultMemberPermissions)
func isInGuild(i *discord.InteractionEvent) bool {
	return i.Member != nil && i.GuildID.IsValid()
}

// Get emojis from database for a server
func getEmojis(serverID int64, offset int, limit int) ([]EmojiData, error) {
	query := `SELECT emote_name, emote_id, usage_count FROM emojis WHERE server_id = ? ORDER BY usage_count DESC LIMIT ? OFFSET ?`
	rows, err := db.Query(query, serverID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var emojis []EmojiData
	for rows.Next() {
		var e EmojiData
		if err := rows.Scan(&e.Name, &e.ID, &e.Count); err != nil {
			return nil, err
		}
		emojis = append(emojis, e)
	}
	return emojis, nil
}

// Get stickers from database for a server
func getStickers(serverID int64, offset int, limit int) ([]StickerData, error) {
	query := `SELECT sticker_name, sticker_id, usage_count FROM stickers WHERE server_id = ? ORDER BY usage_count DESC LIMIT ? OFFSET ?`
	rows, err := db.Query(query, serverID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stickers []StickerData
	for rows.Next() {
		var s StickerData
		if err := rows.Scan(&s.Name, &s.ID, &s.Count); err != nil {
			return nil, err
		}
		stickers = append(stickers, s)
	}
	return stickers, nil
}

// Create pagination buttons
func createPaginationButtons(page, totalPages int, customIDPrefix string) *discord.ActionRowComponent {
	row := discord.ActionRowComponent{}

	if page > 1 {
		row = append(row, &discord.ButtonComponent{
			CustomID: discord.ComponentID(customIDPrefix + ":0"),
			Label:    "<<",
			Style:    discord.PrimaryButtonStyle(),
		},
		)
	}

	if page > 0 {
		row = append(row, &discord.ButtonComponent{
			CustomID: discord.ComponentID(customIDPrefix + ":" + strconv.Itoa(page-1)),
			Label:    "<",
			Style:    discord.PrimaryButtonStyle(),
		})
	}

	row = append(row, &discord.ButtonComponent{
		CustomID: discord.ComponentID("page_display"),
		Label:    fmt.Sprintf("%d/%d", page+1, totalPages),
		Style:    discord.SecondaryButtonStyle(),
		Disabled: true,
	})

	if page < totalPages-1 {
		row = append(row, &discord.ButtonComponent{
			CustomID: discord.ComponentID(customIDPrefix + ":" + strconv.Itoa(page+1)),
			Label:    ">",
			Style:    discord.PrimaryButtonStyle(),
		})
	}
	if page < totalPages-2 {
		row = append(row, &discord.ButtonComponent{
			CustomID: discord.ComponentID(customIDPrefix + ":" + strconv.Itoa(totalPages-1)),
			Label:    ">>",
			Style:    discord.PrimaryButtonStyle(),
		})
	}

	return &row
}

// Create emoji list message
func createEmojiListMessage(emojis []EmojiData, page int) api.InteractionResponseData {
	const perPage = 25
	totalPages := (len(emojis) + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}

	start := page * perPage
	end := start + perPage
	if end > len(emojis) {
		end = len(emojis)
	}

	var content strings.Builder
	content.WriteString("**Custom Emoji Usage Statistics**\n\n")

	if len(emojis) == 0 {
		content.WriteString("No emoji data found for this server.")
	} else {
		for i := start; i < end; i++ {
			e := emojis[i]
			content.WriteString(fmt.Sprintf("- <:%s:%d> **x%d**\n", e.Name, e.ID, e.Count))
		}
	}

	var components discord.ContainerComponents
	if len(emojis) > 0 {
		components = discord.ContainerComponents{
			createPaginationButtons(page, totalPages, "emoji_page"),
		}
	}

	return api.InteractionResponseData{
		Content:    option.NewNullableString(content.String()),
		Components: &components,
		Flags:      discord.EphemeralMessage,
	}
}

// Create sticker list message
func createStickerListMessage(stickers []StickerData, page int) api.InteractionResponseData {
	const perPage = 5
	totalPages := (len(stickers) + perPage - 1) / perPage
	if totalPages == 0 {
		totalPages = 1
	}

	start := page * perPage
	end := start + perPage
	if end > len(stickers) {
		end = len(stickers)
	}

	var content strings.Builder
	content.WriteString("**Sticker Usage Statistics**\n\n")

	var components discord.ContainerComponents
	if len(stickers) > 0 {
		components = discord.ContainerComponents{
			createPaginationButtons(page, totalPages, "sticker_page"),
		}
	}

	embeds := []discord.Embed{}

	for i := start; i < end; i++ {
		s := stickers[i]
		embeds = append(embeds, discord.Embed{
			Title:       fmt.Sprintf("%s x%d", s.Name, s.Count),
			Image:       &discord.EmbedImage{URL: fmt.Sprintf("https://media.discordapp.net/stickers/%d.webp?size=96&quality=lossless", s.ID)},
		})
	}
	
	return api.InteractionResponseData{
		Components: &components,
		Flags:      discord.EphemeralMessage,
		Embeds:     &embeds,
	}
}

// Handle slash commands
func handleCommandInteraction(i *gateway.InteractionCreateEvent) {
	if i.Data.InteractionType() != discord.CommandInteractionType {
		return
	}

	data := i.Data.(*discord.CommandInteraction)

	switch data.Name {
	case "listemotes":
		handleListEmotes(i)
	case "liststickers":
		handleListStickers(i)
	case "resetcount":
		handleResetCount(i)
	}
}

// Handle /listemotes command
func handleListEmotes(i *gateway.InteractionCreateEvent) {
	if !isInGuild(&i.InteractionEvent) {
		respondError(i, "This command can only be used in a server.")
		return
	}

	serverID := int64(i.GuildID)
	emojis, err := getEmojis(serverID, 0, 25)
	if err != nil {
		log.Printf("Error fetching emojis: %v", err)
		respondError(i, "Failed to fetch emoji data.")
		return
	}

	if len(emojis) == 0 {
		respondError(i, "No emoji data found for this server.")
		return
	}

	response := createEmojiListMessage(emojis, 0)
	if err := botState.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &response,
	}); err != nil {
		log.Printf("Error responding to interaction: %v\n%+v", err, response)
	}
}

// Handle /liststickers command
func handleListStickers(i *gateway.InteractionCreateEvent) {
	if !isInGuild(&i.InteractionEvent) {
		respondError(i, "This command can only be used in a server.")
		return
	}

	serverID := int64(i.GuildID)
	stickers, err := getStickers(serverID, 0, 5)
	if err != nil {
		log.Printf("Error fetching stickers: %v", err)
		respondError(i, "Failed to fetch sticker data.")
		return
	}

	if len(stickers) == 0 {
		respondError(i, "No sticker data found for this server.")
		return
	}

	response := createStickerListMessage(stickers, 0)
	if err := botState.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &response,
	}); err != nil {
		log.Printf("Error responding to interaction: %v\n%+v", err, response)
	}
}

// Handle /resetcount command
func handleResetCount(i *gateway.InteractionCreateEvent) {
	if !isInGuild(&i.InteractionEvent) {
		respondError(i, "This command can only be used in a server.")
		return
	}

	serverID := int64(i.GuildID)

	// Reset emoji counts
	_, err1 := db.Exec("DELETE FROM emojis WHERE server_id = ?", serverID)
	// Reset sticker counts
	_, err2 := db.Exec("DELETE FROM stickers WHERE server_id = ?", serverID)

	if err1 != nil || err2 != nil {
		log.Printf("Error resetting counts: %v, %v", err1, err2)
		respondError(i, "Failed to reset counts.")
		return
	}

	response := api.InteractionResponseData{
		Content: option.NewNullableString("✅ All emoji and sticker counts have been reset for this server."),
		Flags:   discord.EphemeralMessage,
	}

	if err := botState.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &response,
	}); err != nil {
		log.Printf("Error responding to interaction: %v\n%+v", err, response)
	}
}

// Handle button interactions for pagination
func handleButtonInteraction(i *gateway.InteractionCreateEvent) {
	if i.Data.InteractionType() != discord.ComponentInteractionType {
		return
	}

	data, ok := i.Data.(*discord.ButtonInteraction)
	if !ok {
		return
	}

	customID := string(data.CustomID)

	// Parse custom ID (format: "emoji_page:0" or "sticker_page:2")
	parts := strings.Split(customID, ":")
	if len(parts) != 2 {
		return
	}

	page, err := strconv.Atoi(parts[1])
	if err != nil {
		return
	}

	serverID := int64(i.GuildID)

	var response api.InteractionResponseData

	if strings.HasPrefix(customID, "emoji_page:") {
		emojis, err := getEmojis(serverID, 25*page, 25)
		if err != nil {
			log.Printf("Error fetching emojis: %v", err)
			return
		}
		response = createEmojiListMessage(emojis, page)
	} else if strings.HasPrefix(customID, "sticker_page:") {
		stickers, err := getStickers(serverID, 5*page, 5)
		if err != nil {
			log.Printf("Error fetching stickers: %v", err)
			return
		}
		response = createStickerListMessage(stickers, page)
	} else {
		return
	}

	if err := botState.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
		Type: api.UpdateMessage,
		Data: &response,
	}); err != nil {
		log.Printf("Error updating message: %v", err)
	}
}

// Helper function to respond with error
func respondError(i *gateway.InteractionCreateEvent, message string) {
	response := api.InteractionResponseData{
		Content: option.NewNullableString("❌ " + message),
		Flags:   discord.EphemeralMessage,
	}

	if err := botState.RespondInteraction(i.ID, i.Token, api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &response,
	}); err != nil {
		log.Printf("Error responding with error: %v", err)
	}
}

// Handle interaction creation events
func handleInteractionCreate(i *gateway.InteractionCreateEvent) {
	// Handle commands and buttons
	switch i.Data.InteractionType() {
	case discord.CommandInteractionType:
		handleCommandInteraction(i)
	case discord.ComponentInteractionType:
		handleButtonInteraction(i)
	}

	// Also track emojis/stickers from interaction messages
	if !i.GuildID.IsValid() {
		return
	}

	serverID := int64(i.GuildID)

	// Check for message in interaction (e.g., button/select menu on a message with emojis)
	if i.Message != nil && i.Message.Content != "" {
		processCustomEmojis(i.Message.Content, serverID)

		if len(i.Message.Stickers) > 0 {
			processStickers(i.Message.Stickers, serverID)
		}
	}
}

// Register application commands
func registerCommands(s *state.State, appID discord.AppID) error {
	manageGuildPerm := discord.NewPermissions(discord.PermissionManageGuild)

	commands := []api.CreateCommandData{
		{
			Name:                     "listemotes",
			Description:              "List custom emoji usage statistics (Moderator only)",
			DefaultMemberPermissions: manageGuildPerm,
		},
		{
			Name:                     "liststickers",
			Description:              "List sticker usage statistics (Moderator only)",
			DefaultMemberPermissions: manageGuildPerm,
		},
		{
			Name:                     "resetcount",
			Description:              "Reset all emoji and sticker counts for this server (Moderator only)",
			DefaultMemberPermissions: manageGuildPerm,
		},
	}

	if _, err := s.BulkOverwriteCommands(appID, commands); err != nil {
		return fmt.Errorf("failed to create command %s: %w", commands[0].Name, err)
	}
	return nil
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	token := os.Getenv("DISCORD_CLIENT_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_CLIENT_TOKEN environment variable is required")
	}

	// Initialize database
	if err := initDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Create a new state
	s := state.NewWithIntents("Bot "+token, gateway.IntentGuildMessages|gateway.IntentMessageContent|gateway.IntentGuildMessageReactions)
	botState = s

	// Add event handlers
	s.AddHandler(handleMessageCreate)
	s.AddHandler(handleInteractionCreate)
	s.AddHandler(handleMessageReactionAdd)
	s.AddHandler(handleMessageReactionRemove)

	s.AddHandler(func(e *gateway.ReadyEvent) {
		log.Printf("Bot is ready! Logged in as %s", e.User.Tag())

		// Register slash commands
		appID := discord.AppID(e.User.ID)

		if err := registerCommands(s, appID); err != nil {
			log.Printf("Failed to register commands: %v", err)
		} else {
			log.Println("All commands registered successfully!")
		}
	})

	// Connect to Discord
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	log.Println("Connecting to Discord...")

	if err := s.Connect(ctx); err != nil && err != context.Canceled {
		log.Fatalf("Failed to connect: %v", err)
	}
	<-ctx.Done()
	log.Println("Shutting down...")
}
