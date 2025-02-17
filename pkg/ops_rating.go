package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func changeBookRating() {
	//getting newRating from environment variable
	newRating := os.Getenv("newRating")
	if newRating == "0" {
		newRating = ""
	}
	// get the user_book_id environment variable
	var queryString string

	bookID := os.Getenv("current_bookID")
	if bookID != "" {
		//convert bookID to int
		bookIDInt, err := strconv.Atoi(bookID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error converting bookID: %s", err)
		}
		queryString = fmt.Sprintf(`insert_user_book(object: {book_id: %v, rating: "%s"})`, bookIDInt, newRating)
	} else {
		fmt.Fprintf(os.Stderr, "No user_bookID or bookID found")
	}

	query := fmt.Sprintf(`mutation {
		%s {
			id
			error
			user_book {
			book {
				id
			}
			}
		}
		}`, queryString)
	// fmt.Fprintf(os.Stderr, "%s", query)
	interrogateAPI(query)
	if newRating == "" {
		notificationString := "Book rating removed!🚀"
		fmt.Println(notificationString)
	} else {
		notificationString := fmt.Sprintf("Book rating changed to %v⭐️!🚀", newRating)
		fmt.Println(notificationString)
	}
}

func fetchServeRating() ([]byte, error) {
	// a function to serve user's library book count by rating
	// Start timing
	startTime := time.Now()

	// Open SQLite database
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		LogF("ERROR")
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}
	defer db.Close()

	query := `
	SELECT * FROM ratings
	ORDER BY rating DESC;`

	// Execute the query
	rows, err := db.Query(query)

	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	var currentBookID int
	var currentRatingFloat float64
	// currentBookIDstr :=
	currentSearchString := os.Getenv("mySearchString")
	// currentRating :=
	backString := "🏡 library search"

	subtitleString := "↩️ to list"
	breadCrumb := "listRatings"

	// Create the result object
	result := map[string][]map[string]interface{}{
		"items": {},
	}
	p := message.NewPrinter(language.English)

	// Iterate through the rows from the database
	for rows.Next() {

		// if there is a BookID, the goal is to assign or change the rating
		var rating float64
		var ratingSymbol string
		var count, id_rating int
		myIcon := "icons/blueStar.png"
		if err := rows.Scan(&id_rating, &rating, &count); err != nil {
			LogF("failed to scan row: %v", err)
		}
		titleString := p.Sprintf("Books I rated %.1f⭐️ (%d) %s", rating, count, ratingSymbol)
		if os.Getenv("current_bookID") != "" {
			currentBookID, err = strconv.Atoi(os.Getenv("current_bookID"))
			backString = "⬅️ back to library search"
			breadCrumb = ""
			titleString = p.Sprintf("Rate %.1f⭐️ (%d) %s", rating, count, ratingSymbol)
			subtitleString = "↩️ to rate"
			if err != nil {
				LogF("Invalid current_bookID: %v", err)
			}
			// if there is a currentRating, the goal is to change it.
			if os.Getenv("current_rating") != "" {
				currentRatingFloat, err = strconv.ParseFloat(os.Getenv("current_rating"), 64)
				if err != nil {
					LogF("Invalid current_rating: %v", err)
				}
			}
		}

		// if a bookID is defined, I want to change rating
		if currentBookID != 0 {
			if rating == currentRatingFloat {
				ratingSymbol = RatingEmoji[rating]
				titleString = p.Sprintf("Current rating: %.1f⭐️ (%d) %s", rating, count, ratingSymbol)
				subtitleString = ""
				myIcon = "icons/redStar.png"
			}
		} else {
			ratingSymbol = ""
		}
		if rating == 0 {
			titleString = p.Sprintf("Books without rating (%d)", count)
			myIcon = "icons/grayStar.png"
		}

		// Append data to the result
		result["items"] = append(result["items"], map[string]interface{}{
			"title":    titleString,
			"subtitle": subtitleString,
			"valid":    true,
			"variables": map[string]interface{}{
				"newRating":  rating,
				"breadCrumb": breadCrumb,
			},
			"icon": map[string]string{
				"path": myIcon,
			},
			"mods": map[string]interface{}{

				"ctrl": map[string]interface{}{
					"valid":    true,
					"arg":      "",
					"subtitle": "",
				},
				"alt": map[string]interface{}{
					"valid":    true,
					"arg":      "",
					"subtitle": "",
				},
				"cmd": map[string]interface{}{
					"valid":    true,
					"arg":      "",
					"subtitle": "",
				},
				"cmd+alt": map[string]interface{}{
					"subtitle": backString,
					"valid":    true,
					"arg":      currentSearchString,
					"variables": map[string]interface{}{
						"newRating":  "",
						"breadCrumb": "",
					},
				},
			},
			"arg": "",
		})
	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during rows iteration: %w", err)
	}

	// Convert the result to JSON
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode JSON: %w", err)
	}
	// Calculate and log execution time
	elapsedTime := time.Since(startTime)
	LogF("Execution time: %d ms", elapsedTime.Milliseconds())
	fmt.Println(string(jsonData))
	return jsonData, nil
}
