// decouvertes.go
//
// This is the core CLI engine for the decouvertes leitner box game.
// It's written in Go as an excuse to dive into the language.
// This version adds support for multiple players and detailed stat tracking.

package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// --- Structs for Data Modeling ---

// Card represents a single flashcard from cards.json.
type Card struct {
	ID       string   `json:"id"`
	Language string   `json:"language"`
	Tags     []string `json:"tags"`
	Prompt   string   `json:"prompt"`
	Solution string   `json:"solution"`
}

// CardProgress represents the user's progress on a single card.
type CardProgress struct {
	Box          int       `json:"box"`
	Streak       int       `json:"streak"`
	Passed       int       `json:"passed"`
	Failed       int       `json:"failed"`
	LastReviewed time.Time `json:"last_reviewed"`
}

// AnswerLogItem records a single answer event.
type AnswerLogItem struct {
	CardID    string    `json:"card_id"`
	Timestamp time.Time `json:"timestamp"`
	Correct   bool      `json:"correct"`
}

// PlayerData holds all data for a single player.
type PlayerData struct {
	Name          string                  `json:"name"`
	TotalAnswered int                     `json:"total_answered"`
	Cards         map[string]CardProgress `json:"cards"`
	History       []AnswerLogItem         `json:"history"`
}

// CheckResult is the structure returned as JSON after checking an answer.
type CheckResult struct {
	Correct  bool   `json:"correct"`
	NewBox   int    `json:"new_box"`
	Solution string `json:"solution"`
}

// --- Main Function: Entry Point ---

func main() {
	// Note: rand.Seed() is not needed in Go 1.20+

	// Define our subcommands
	getCardCmd := flag.NewFlagSet("get-card", flag.ExitOnError)
	checkAnswerCmd := flag.NewFlagSet("check-answer", flag.ExitOnError)
	createPlayerCmd := flag.NewFlagSet("create-player", flag.ExitOnError)
	listPlayersCmd := flag.NewFlagSet("list-players", flag.ExitOnError)
	deletePlayerCmd := flag.NewFlagSet("delete-player", flag.ExitOnError)
	getStatsCmd := flag.NewFlagSet("get-stats", flag.ExitOnError)

	// Flags for commands that require a player ID
	playerIDGet := getCardCmd.String("player-id", "", "The ID of the player (required).")
	playerIDCheck := checkAnswerCmd.String("player-id", "", "The ID of the player (required).")
	playerIDDelete := deletePlayerCmd.String("player-id", "", "The ID of the player to delete (required).")
	playerIDStats := getStatsCmd.String("player-id", "", "The ID of the player to get stats for (required).")

	// Flags for specific commands
	cardID := checkAnswerCmd.String("id", "", "The ID of the card being answered (required).")
	userAnswer := checkAnswerCmd.String("answer", "", "The user's answer (required).")
	playerName := createPlayerCmd.String("name", "", "The name for the new player (required).")

	if len(os.Args) < 2 {
		log.Fatal("Expected 'get-card', 'check-answer', 'create-player', 'list-players', 'delete-player', or 'get-stats' subcommands.")
	}

	// Route to the correct handler
	switch os.Args[1] {
	case "get-card":
		getCardCmd.Parse(os.Args[2:])
		if *playerIDGet == "" {
			log.Fatal("--player-id flag is required")
		}
		handleGetCard(*playerIDGet)
	case "check-answer":
		checkAnswerCmd.Parse(os.Args[2:])
		if *playerIDCheck == "" || *cardID == "" || *userAnswer == "" {
			log.Fatal("--player-id, --id, and --answer flags are required")
		}
		handleCheckAnswer(*playerIDCheck, *cardID, *userAnswer)
	case "create-player":
		createPlayerCmd.Parse(os.Args[2:])
		if *playerName == "" {
			log.Fatal("--name flag is required")
		}
		handleCreatePlayer(*playerName)
	case "list-players":
		listPlayersCmd.Parse(os.Args[2:])
		handleListPlayers()
	case "delete-player":
		deletePlayerCmd.Parse(os.Args[2:])
		if *playerIDDelete == "" {
			log.Fatal("--player-id flag is required")
		}
		handleDeletePlayer(*playerIDDelete)
	case "get-stats":
		getStatsCmd.Parse(os.Args[2:])
		if *playerIDStats == "" {
			log.Fatal("--player-id flag is required")
		}
		handleGetStats(*playerIDStats)
	default:
		log.Fatalf("Unknown subcommand: %s.", os.Args[1])
	}
}

// --- Command Handlers ---

func handleGetCard(playerID string) {
	cards := loadCards()
	allProgress := loadAllProgress()
	playerProgress, ok := allProgress[playerID]
	if !ok {
		log.Fatalf("Player with ID '%s' not found.", playerID)
	}

	progressUpdated := false
	for _, card := range cards {
		if _, ok := playerProgress.Cards[card.ID]; !ok {
			playerProgress.Cards[card.ID] = CardProgress{Box: 1, Streak: 0, Passed: 0, Failed: 0, LastReviewed: time.Now()}
			progressUpdated = true
		}
	}
	if progressUpdated {
		allProgress[playerID] = playerProgress
		saveAllProgress(allProgress)
	}

	boxes := make(map[int][]Card)
	for _, card := range cards {
		p := playerProgress.Cards[card.ID]
		if p.Box > 0 && p.Box <= 5 {
			boxes[p.Box] = append(boxes[p.Box], card)
		}
	}

	weights := map[int]int{1: 16, 2: 8, 3: 4, 4: 2, 5: 1}
	totalWeight := 0
	for boxNum, cardList := range boxes {
		if len(cardList) > 0 {
			totalWeight += weights[boxNum]
		}
	}

	if totalWeight == 0 {
		fmt.Println(`{"prompt": "Congratulations, you have mastered all cards!", "id": "done"}`)
		return
	}

	r := rand.Intn(totalWeight)
	chosenBox := 0
	for i := 1; i <= 5; i++ {
		if weight, ok := weights[i]; ok && len(boxes[i]) > 0 {
			if r < weight {
				chosenBox = i
				break
			}
			r -= weight
		}
	}

	chosenCardIndex := rand.Intn(len(boxes[chosenBox]))
	chosenCard := boxes[chosenBox][chosenCardIndex]

	jsonOutput, err := json.Marshal(chosenCard)
	if err != nil {
		log.Fatalf("Error marshalling card to JSON: %v", err)
	}
	fmt.Println(string(jsonOutput))
}

func handleCheckAnswer(playerID, cardID, userAnswer string) {
	cards := loadCards()
	allProgress := loadAllProgress()
	playerProgress, ok := allProgress[playerID]
	if !ok {
		log.Fatalf("Player with ID '%s' not found.", playerID)
	}

	var targetCard Card
	found := false
	for _, c := range cards {
		if c.ID == cardID {
			targetCard = c
			found = true
			break
		}
	}
	if !found {
		log.Fatalf("Card with ID '%s' not found.", cardID)
	}

	isCorrect := normalizeString(userAnswer) == normalizeString(targetCard.Solution)

	// Update card and player stats
	cardProgress := playerProgress.Cards[cardID]
	playerProgress.TotalAnswered++
	if isCorrect {
		cardProgress.Box++
		cardProgress.Streak++
		cardProgress.Passed++
	} else {
		cardProgress.Box = 1
		cardProgress.Streak = 0
		cardProgress.Failed++
	}
	cardProgress.LastReviewed = time.Now()
	playerProgress.Cards[cardID] = cardProgress

	// Add a new entry to the history log
	playerProgress.History = append(playerProgress.History, AnswerLogItem{
		CardID:    cardID,
		Timestamp: time.Now(),
		Correct:   isCorrect,
	})

	allProgress[playerID] = playerProgress
	saveAllProgress(allProgress)

	result := CheckResult{
		Correct:  isCorrect,
		NewBox:   cardProgress.Box,
		Solution: targetCard.Solution,
	}
	jsonOutput, err := json.Marshal(result)
	if err != nil {
		log.Fatalf("Error marshalling result to JSON: %v", err)
	}
	fmt.Println(string(jsonOutput))
}

func handleCreatePlayer(name string) {
	allProgress := loadAllProgress()
	newID := generateUniqueID()

	allProgress[newID] = PlayerData{
		Name:          name,
		TotalAnswered: 0,
		Cards:         make(map[string]CardProgress),
		History:       make([]AnswerLogItem, 0),
	}

	saveAllProgress(allProgress)
	fmt.Println(newID)
}

func handleListPlayers() {
	allProgress := loadAllProgress()
	if len(allProgress) == 0 {
		fmt.Println("No players found. Create one with 'create-player --name=\"YourName\"'")
		return
	}
	for id, data := range allProgress {
		fmt.Printf("Name: %s, ID: %s\n", data.Name, id)
	}
}

func handleDeletePlayer(playerID string) {
	allProgress := loadAllProgress()
	if _, ok := allProgress[playerID]; !ok {
		log.Fatalf("Player with ID '%s' not found.", playerID)
	}

	delete(allProgress, playerID)
	saveAllProgress(allProgress)
	fmt.Printf("Player with ID '%s' has been deleted.\n", playerID)
}

func handleGetStats(playerID string) {
	allProgress := loadAllProgress()
	player, ok := allProgress[playerID]
	if !ok {
		log.Fatalf("Player with ID '%s' not found.", playerID)
	}

	// --- Basic Stats ---
	totalPassed := 0
	totalFailed := 0
	for _, cardProgress := range player.Cards {
		totalPassed += cardProgress.Passed
		totalFailed += cardProgress.Failed
	}

	fmt.Printf("Stats for Player: %s\n", player.Name)
	fmt.Println("-------------------------")
	fmt.Printf("Total Cards Answered: %d\n", player.TotalAnswered)
	fmt.Printf("Correct Answers: %d\n", totalPassed)
	fmt.Printf("Incorrect Answers: %d\n", totalFailed)

	if len(player.History) == 0 {
		fmt.Println("\nNo historical data to analyze yet.")
		return
	}

	// --- Time-based Stats ---
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	cardsToday := 0
	for _, item := range player.History {
		if item.Timestamp.After(todayStart) {
			cardsToday++
		}
	}
	fmt.Printf("Cards Answered Today: %d\n", cardsToday)

	// --- Daily Streak Calculation ---
	if len(player.History) > 0 {
		// Create a set of unique days the player was active
		activeDays := make(map[time.Time]bool)
		for _, item := range player.History {
			day := time.Date(item.Timestamp.Year(), item.Timestamp.Month(), item.Timestamp.Day(), 0, 0, 0, 0, time.UTC)
			activeDays[day] = true
		}

		// Sort the unique days
		sortedDays := make([]time.Time, 0, len(activeDays))
		for day := range activeDays {
			sortedDays = append(sortedDays, day)
		}
		sort.Slice(sortedDays, func(i, j int) bool {
			return sortedDays[i].Before(sortedDays[j])
		})

		longestStreak := 0
		currentStreak := 0
		if len(sortedDays) > 0 {
			longestStreak = 1
			currentStreak = 1
			for i := 1; i < len(sortedDays); i++ {
				// Check if the current day is exactly one day after the previous
				if sortedDays[i].Sub(sortedDays[i-1]).Hours() == 24 {
					currentStreak++
				} else {
					currentStreak = 1 // Streak is broken
				}
				if currentStreak > longestStreak {
					longestStreak = currentStreak
				}
			}
		}
		fmt.Printf("Longest Daily Streak: %d day(s)\n", longestStreak)
	}
}

// --- File I/O and Helper Functions ---

func getConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Could not find user home directory: %v", err)
	}
	return filepath.Join(home, ".config", "decouvertes")
}

func loadCards() []Card {
	configDir := getConfigDir()
	filePath := filepath.Join(configDir, "cards.json")
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		log.Fatalf("Config directory not found at %s. Please create it and place your 'cards.json' file inside.", configDir)
	}
	file, err := ioutil.ReadFile(filePath)
	if err != nil {
		log.Fatalf("Error reading file (%s): %v.", filePath, err)
	}
	var cards []Card
	if err := json.Unmarshal(file, &cards); err != nil {
		log.Fatalf("Error unmarshalling cards JSON: %v", err)
	}
	return cards
}

func loadAllProgress() map[string]PlayerData {
	progress := make(map[string]PlayerData)
	configDir := getConfigDir()
	filePath := filepath.Join(configDir, "progress.json")
	file, err := ioutil.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return progress
		}
		log.Fatalf("Error reading progress file (%s): %v", filePath, err)
	}
	if len(file) == 0 {
		return progress
	}
	if err := json.Unmarshal(file, &progress); err != nil {
		log.Fatalf("Error unmarshalling progress JSON: %v", err)
	}
	return progress
}

func saveAllProgress(progress map[string]PlayerData) {
	configDir := getConfigDir()
	filePath := filepath.Join(configDir, "progress.json")
	data, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling progress to JSON: %v", err)
	}
	if err := ioutil.WriteFile(filePath, data, 0644); err != nil {
		log.Fatalf("Error writing progress file (%s): %v", filePath, err)
	}
}

func normalizeString(s string) string {
	lower := strings.ToLower(s)
	noSpace := strings.Join(strings.Fields(lower), "")
	noSemicolon := strings.TrimRight(noSpace, ";")
	return noSemicolon
}

func generateUniqueID() string {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		log.Fatalf("Failed to generate unique ID: %v", err)
	}
	return hex.EncodeToString(bytes)
}
