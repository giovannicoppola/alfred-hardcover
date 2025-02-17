package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const apiRoot = "https://api.hardcover.app/v1/graphql"

const baseURL = "https://hardcover.app/books/"
const listURL = "https://hardcover.app/@giovanni/lists/"

// Get the system temporary directory
var tempDir = os.TempDir()

// Declare package-level variables
var (
	numberResults int
	databasePath  string
	dataFolder    string
	authToken     string
	coverDir      string
	whereClauses  []string
	userID        int
	username      string
	lastUpdated   string
)

func parseUserID(APIresponse []byte) {

	type Me struct {
		ID        int    `json:"id"`
		UpdatedAt string `json:"updated_at"`
		Username  string `json:"username"`
	}

	type Data struct {
		Me []Me `json:"me"`
	}

	type Response struct {
		Data Data `json:"data"`
	}
	// Declare a variable to hold the unmarshalled data
	var response Response

	// Unmarshal the JSON into the struct
	err := json.Unmarshal([]byte(APIresponse), &response)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error unmarshalling JSON: %v\n", err)

	}

	// Extract the user ID
	if len(response.Data.Me) > 0 {
		userID = response.Data.Me[0].ID
		username = response.Data.Me[0].Username
		lastUpdated = response.Data.Me[0].UpdatedAt
	} else {
		LogF("User ID not found")
	}

}

func fetchUserIDfile() {
	userIDFile := filepath.Join(dataFolder, "userID")
	// File doesn't exist, fetch userID from the server
	APIresponse, err := interrogateAPI(`query { me {
		id
		username
		updated_at
		 } }`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch userID: %v\n", err)
		return
	}

	parseUserID(APIresponse)

	err = os.WriteFile(userIDFile, []byte(APIresponse), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write userID to file: %v\n", err)
		return
	}

}

func checkUserIDFile() {
	userIDFile := filepath.Join(dataFolder, "userID")

	if _, err := os.Stat(userIDFile); os.IsNotExist(err) {
		LogF("User ID file not found, creating")
		fetchUserIDfile()
	} else {
		// File exists, read userID from the file
		data, err := os.ReadFile(userIDFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read userID from file: %v\n", err)
			return
		}

		parseUserID(data)
	}
}

// isBeyondToday reads lastUpdatedLocal from file, adds CheckRate days, and compares with timestamp
func isBeyondToday() (bool, error) {
	// Read lastUpdatedLocal from file
	lastUpdateLocalFile := filepath.Join(dataFolder, "lastUpdatedLocal")
	data, err := os.ReadFile(lastUpdateLocalFile)
	if err != nil {
		return false, fmt.Errorf("failed to read lastUpdatedLocal file: %w", err)
	}
	lastUpdatedStr := string(data)
	// LogF("Last Updated Local:", lastUpdatedStr)

	// Parse lastUpdatedLocal timestamp
	lastUpdatedLocal, err := time.Parse("2006-01-02T15:04:05.000000-07:00", lastUpdatedStr)
	if err != nil {
		return false, fmt.Errorf("failed to parse lastUpdatedLocal timestamp: %w", err)
	}
	checkRate, err := strconv.Atoi(os.Getenv("CHECKRATE"))
	if err != nil {
		LogF("Error in getting CHECKRATE:", err)

	}
	// Add checkRate days to lastUpdated
	updatedTime := lastUpdatedLocal.Add(time.Duration(checkRate) * 24 * time.Hour)

	// Check if updatedTime is after today
	if updatedTime.Before(time.Now()) {
		LogF("Updating the userID file")
		fetchUserIDfile() // Update the userID file

		lastUpdatedRemote, err := time.Parse("2006-01-02T15:04:05.000000-07:00", lastUpdated)

		if err != nil {
			return false, fmt.Errorf("failed to parse lastUpdatedRemote timestamp: %w", err)
		}
		if lastUpdatedRemote.After(lastUpdatedLocal) {

			LogF("Database is outdated, refreshing")
			createLibraryDatabase()
			os.Exit(0)
		} else {
			LogF("Database is up to date")
		}
	}
	return true, nil
}

func init() {
	var err error // Declare err at function scope to prevent shadowing

	// Get RESULT_LENGTH from environment, default to 9 if invalid
	numberResults, err = strconv.Atoi(os.Getenv("RESULT_LENGTH"))
	if err != nil {
		numberResults = 9
	}

	// Get API token
	authToken = os.Getenv("HARDCOVER_API_TOKEN")
	if authToken == "" {
		LogF("Please set the HARDCOVER_API_TOKEN environment variable.")
		return
	}

	// Get and create data folder
	dataFolder = os.Getenv("alfred_workflow_data")
	if err = os.MkdirAll(dataFolder, os.ModePerm); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create data folder: %v\n", err)
		return
	}

	databasePath = filepath.Join(dataFolder, "books.db")

	// Set and create cover directory
	coverDir = filepath.Join(dataFolder, "covers")
	if err = os.MkdirAll(coverDir, os.ModePerm); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create covers directory: %v\n", err)
		return
	}

	// Check if the user ID file exists,if not create one
	checkUserIDFile()

	_, err = os.Stat(databasePath)
	if os.IsNotExist(err) {
		// Create the database
		createLibraryDatabase()
		os.Exit(0)
	}

	_, err = isBeyondToday()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

}
