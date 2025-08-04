// decouvertes.go
//
// This is the core CLI engine for the Découvertes Leitner Box game.
// It's written in Go for high performance and instant startup.
//
// It now looks for its data files (`cards.json`, `progress.json`) in a
// dedicated configuration directory: ~/.config/decouvertes/

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath" // <-- Added for path manipulation
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

// ProgressItem represents the user's progress on a single card.
type ProgressItem struct {
	Box          int       `json:"box"`
	Streak       int       `json:"streak"`
	LastReviewed time.Time `json:"last_reviewed"`
}

// CheckResult is the structure returned as JSON after checking an answer.
type CheckResult struct {
	Correct  bool   `json:"correct"`
	NewBox   int    `json:"new_box"`
	Solution string `json:"solution"`
}

// --- Main Function: Entry Point ---

func main() {
	rand.Seed(time.Now().UnixNano())

	getCardCmd := flag.NewFlagSet("get-card", flag.ExitOnError)
	checkAnswerCmd := flag.NewFlagSet("check-answer", flag.ExitOnError)

	cardID := checkAnswerCmd.String("id", "", "The ID of the card being answered (required).")
	userAnswer := checkAnswerCmd.String("answer", "", "The user's answer (required).")

	if len(os.Args) < 2 {
		log.Fatal("Expected 'get-card' or 'check-answer' subcommands.")
	}

	switch os.Args[1] {
	case "get-card":
		getCardCmd.Parse(os.Args[2:])
		handleGetCard()
	case "check-answer":
		checkAnswerCmd.Parse(os.Args[2:])
		if *cardID == "" || *userAnswer == "" {
			log.Fatal("--id and --answer flags are required for check-answer")
		}
		handleCheckAnswer(*cardID, *userAnswer)
	default:
		log.Fatalf("Unknown subcommand: %s. Expected 'get-card' or 'check-answer'.", os.Args[1])
	}
}

// --- Command Handlers ---

func handleGetCard() {
	cards := loadCards()
	progress := loadProgress()

	progressUpdated := false
	for _, card := range cards {
		if _, ok := progress[card.ID]; !ok {
			progress[card.ID] = ProgressItem{Box: 1, Streak: 0, LastReviewed: time.Now()}
			progressUpdated = true
		}
	}
	if progressUpdated {
		saveProgress(progress)
	}

	boxes := make(map[int][]Card)
	for _, card := range cards {
		p := progress[card.ID]
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

func handleCheckAnswer(cardID, userAnswer string) {
	cards := loadCards()
	progress := loadProgress()

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

	currentProgress := progress[cardID]
	if isCorrect {
		currentProgress.Box++
		currentProgress.Streak++
	} else {
		currentProgress.Box = 1
		currentProgress.Streak = 0
	}
	currentProgress.LastReviewed = time.Now()
	progress[cardID] = currentProgress

	saveProgress(progress)

	result := CheckResult{
		Correct:  isCorrect,
		NewBox:   currentProgress.Box,
		Solution: targetCard.Solution,
	}
	jsonOutput, err := json.Marshal(result)
	if err != nil {
		log.Fatalf("Error marshalling result to JSON: %v", err)
	}
	fmt.Println(string(jsonOutput))
}

// --- File I/O and Helper Functions ---

// getConfigDir returns the path to the config directory (~/.config/decouvertes).
func getConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("Could not find user home directory: %v", err)
	}
	return filepath.Join(home, ".config", "decouvertes")
}

// loadData reads a file from the config directory.
func loadData(filename string) []byte {
	configDir := getConfigDir()
	filePath := filepath.Join(configDir, filename)

	// Check if the config directory exists.
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		log.Fatalf("Config directory not found at %s. Please create it and place your '%s' file inside.", configDir, filename)
	}

	file, err := ioutil.ReadFile(filePath)
	if err != nil {
		// Handle case where the file itself doesn't exist, which is okay for progress.json
		if os.IsNotExist(err) && filename == "progress.json" {
			return nil // Return nil, the caller will handle creating a new progress map.
		}
		log.Fatalf("Error reading file (%s): %v.", filePath, err)
	}
	return file
}

func loadCards() []Card {
	fileBytes := loadData("cards.json")
	if fileBytes == nil {
		log.Fatal("cards.json is missing or empty.")
	}

	var cards []Card
	if err := json.Unmarshal(fileBytes, &cards); err != nil {
		log.Fatalf("Error unmarshalling cards JSON: %v", err)
	}
	return cards
}

func loadProgress() map[string]ProgressItem {
	progress := make(map[string]ProgressItem)
	fileBytes := loadData("progress.json")
	if fileBytes == nil || len(fileBytes) == 0 {
		return progress // Return empty map if file doesn't exist or is empty.
	}

	if err := json.Unmarshal(fileBytes, &progress); err != nil {
		log.Fatalf("Error unmarshalling progress JSON: %v", err)
	}
	return progress
}

func saveProgress(progress map[string]ProgressItem) {
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
