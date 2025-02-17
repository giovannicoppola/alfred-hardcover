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

func changeBookStatus() {
	//getting newStatus from environment variable
	newStatusInt, err := strconv.Atoi(os.Getenv("newStatus"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error converting newStatus: %s", err)
	}

	// get the user_book_id environment variable
	userBookID := os.Getenv("current_user_bookID")
	var queryString string

	userBookIDInt, err := strconv.Atoi(userBookID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error converting userBookID: %s", err)
	}

	if userBookIDInt > 0 {
		//convert userBookID to int

		queryString = fmt.Sprintf(`update_user_book(id: %v, object: {status_id: %v})`, userBookIDInt, newStatusInt)

	} else {
		bookID := os.Getenv("current_bookID")
		LogF("BookID: %s", bookID)
		if bookID != "" {
			//convert bookID to int
			bookIDInt, err := strconv.Atoi(bookID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error converting bookID: %s", err)
			}
			queryString = fmt.Sprintf(`insert_user_book(object: {book_id: %v, status_id: %v})`, bookIDInt, newStatusInt)
		} else {
			fmt.Fprintf(os.Stderr, "No user_bookID or bookID found")
		}

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
	notificationString := fmt.Sprintf("Book status changed to '%s'.", ReadStatus[newStatusInt])
	fmt.Println(notificationString)
}

func fetchServeStatus() ([]byte, error) {
	// a function to serve user's library book count by status
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
	WITH RECURSIVE status_range AS (
    SELECT 0 AS status_id
    UNION ALL
    SELECT status_id + 1 FROM status_range WHERE status_id < 4
	)
	SELECT sr.status_id, 
		COUNT(b.status_id) AS count
	FROM status_range sr
	LEFT JOIN books b ON sr.status_id = b.status_id
	GROUP BY sr.status_id
	ORDER BY sr.status_id;`

	// Execute the query
	rows, err := db.Query(query)

	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Get the current_book_id (if present) from the environment variable
	// currentBookIDStr := os.Getenv("current_bookID")
	currentStatusID := -1
	currentStatusIDstr := os.Getenv("current_statusID")
	if currentStatusIDstr != "" {
		currentStatusID, err = strconv.Atoi(currentStatusIDstr)

		if err != nil {
			LogF("Invalid current_statusID: %v", err)
		}
	}

	currentBookID := 0
	currentBookIDstr := os.Getenv("current_bookID")
	currentSearchString := os.Getenv("mySearchString")
	backString := "🏡 library search"

	if currentBookIDstr != "" {
		currentBookID, err = strconv.Atoi(os.Getenv("current_bookID"))
		backString = "⬅️ back to library search"
		if err != nil {
			LogF("Invalid current_bookID: %v", err)
		}
	}
	// Create the result object
	result := map[string][]map[string]interface{}{
		"items": {},
	}
	for rows.Next() {

		// Iterate through the rows from the database
		for rows.Next() {
			var statusID, count int
			if err := rows.Scan(&statusID, &count); err != nil {
				LogF("failed to scan row: %v", err)
			}
			// LogF("Status ID: %d (%s), Count: %d", statusID, ReadStatus[statusID], count)

			// skip the current status, if there is one
			if statusID == currentStatusID {
				continue
			}
			p := message.NewPrinter(language.English)

			subtitleAdd := fmt.Sprintf("↩️ to list %s", ReadStatus[statusID])
			breadCrumb := "listStatusBooks"

			// if a bookID is defined, I want to show which shelves the book is on
			if currentStatusID >= 0 || currentBookID != 0 {
				subtitleAdd = fmt.Sprintf("↩️ to add to '%s'", ReadStatus[statusID])
				breadCrumb = ""
				if err != nil {
					LogF("Invalid current_book_id: %v", err)
				}
			}

			// Append data to the result
			result["items"] = append(result["items"], map[string]interface{}{
				"title":    p.Sprintf("%s (%d)", ReadStatus[statusID], count),
				"subtitle": p.Sprintf("%s", subtitleAdd),
				"valid":    true,
				"variables": map[string]interface{}{
					"newStatus":  statusID,
					"breadCrumb": breadCrumb,
				},
				"icon": map[string]string{
					"path": ReadStatusIcon[statusID],
				},
				"mods": map[string]interface{}{

					"cmd": map[string]interface{}{
						"valid":    true,
						"arg":      "listURL + slug",
						"subtitle": "️open list on Hardcover",
					},
					"cmd+alt": map[string]interface{}{
						"subtitle": backString,
						"valid":    true,
						"arg":      currentSearchString,
						"variables": map[string]interface{}{
							"newStatus":  "",
							"breadCrumb": "",
						},
					},
				},
				"arg": "",
			})
		}
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
