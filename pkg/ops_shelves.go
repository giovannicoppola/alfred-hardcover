package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func fetchUserShelves() ([]byte, error) {
	query := `query {me {lists {
		name
		id
		books_count
		public
		slug
	  }
	}}`
	shelves, err := interrogateAPI(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to fetch shelves:", err)
		return nil, err
	}

	var shelfData ShelfJSON
	err = json.Unmarshal(shelves, &shelfData)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error decoding JSON response:", err)
		return nil, err
	}

	// Marshal shelfData into []byte (JSON)
	shelfJSON, err := json.Marshal(shelfData)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error marshalling JSON:", err)
		return nil, err
	}

	// Return the marshaled JSON data as []byte
	return shelfJSON, nil
}

func fetchServeShelves() ([]byte, error) {
	// Start timing
	startTime := time.Now()

	// Open SQLite database
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		LogF("ERROR")
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}
	defer db.Close()

	// Fetch shelves being used
	shelves, err := fetchShelves(db)
	if err != nil {
		log.Fatal(err)
	}

	// retrieve all shelves from the bookshelves table
	query := `
	SELECT *, COUNT(*) OVER () AS total_count
	 FROM bookshelves
	 ORDER BY books_count DESC
	
	`

	// Execute the query
	rows, err := db.Query(query)

	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	shelfCount := 0
	// Get the current_book_id (if present) from the environment variable
	currentBookIDStr := os.Getenv("current_bookID")

	currentSearchString := os.Getenv("mySearchString")
	backString := "🏡 library search"
	if currentBookIDStr != "" {
		backString = "⬅️ back to library search"
	}

	// Create the result object
	result := map[string][]map[string]interface{}{
		"items": {},
	}

	// Iterate through the rows from the database
	for rows.Next() {
		var name, slug string
		var shelf_id, booksCount, total_count int
		var public bool
		shelfCount++

		err := rows.Scan(&shelf_id, &name, &booksCount, &public, &slug, &total_count)
		if err != nil {
			LogF("failed to scan row: %v", err)
			continue
		}
		p := message.NewPrinter(language.English)

		bookShelfSymbol := ""
		subtitleAdd := "↩️ to list books"
		shelfAction := ""
		breadCrumb := "listShelfBooks"

		// if a bookID is defined, I want to show which shelves the book is on
		if currentBookIDStr != "" {
			// Convert current_book_id to an integer
			currentBookID, err := strconv.Atoi(currentBookIDStr)
			shelfAction = "addList"
			subtitleAdd = "↩️ to add to shelf"
			breadCrumb = ""
			if err != nil {
				LogF("Invalid current_book_id: %v", err)
			}
			// Run the check
			if isBookOnShelf(shelves, currentBookID, shelf_id) {
				bookShelfSymbol = "📚"
				subtitleAdd = "↩️ to remove from shelf"
				shelfAction = "removeList"

			}
		}

		// Append data to the result
		result["items"] = append(result["items"], map[string]interface{}{
			"title":    p.Sprintf("%s (%d) %s", name, booksCount, bookShelfSymbol),
			"subtitle": p.Sprintf("%d/%d %s", shelfCount, total_count, subtitleAdd),
			"valid":    true,
			"variables": map[string]interface{}{
				"current_listID":    shelf_id,
				"current_shelfName": name,
				"breadCrumb":        breadCrumb,
			},
			"icon": map[string]string{
				"path": "icons/shelf.png",
			},
			"mods": map[string]interface{}{

				"alt": map[string]interface{}{
					"valid":    true,
					"arg":      "",
					"subtitle": "️",
				},
				"ctrl": map[string]interface{}{
					"valid":    true,
					"arg":      listURL + slug,
					"subtitle": "️open list on Hardcover",
				},
				"cmd+alt": map[string]interface{}{
					"subtitle": backString,
					"valid":    true,
					"arg":      currentSearchString,
					"variables": map[string]interface{}{
						"breadCrumb": "",
					},
				},
			},
			"arg": shelfAction,
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

func toggleShelf(shelfAction string) {
	// Open SQLite database
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to open SQLite database:", err)
		return
	}

	defer db.Close()

	bookID, err := strconv.Atoi(os.Getenv("current_bookID"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid book ID:", err)
		return
	}
	listID, err := strconv.Atoi(os.Getenv("current_listID"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid list ID:", err)
		return
	}

	shelfName := os.Getenv("current_shelfName")
	query := ""
	notificationMessage := ""
	switch shelfAction {
	case "addList":
		{
			query = fmt.Sprintf(`mutation{
	insert_list_book(object: {book_id: %d, list_id: %d}) {
	id }}`, bookID, listID)
			notificationMessage = fmt.Sprintf("Book added to the %s shelf.", shelfName)
		}
	case "removeList":
		{
			myUserListBookID, listName, err := getUserListBookID(db, bookID, listID)
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error getting user_book_id:", err)
				return
			}

			query = fmt.Sprintf(`mutation {
		delete_list_book(id: %d) {
		  id
		  list_id
		}
	  }`, myUserListBookID)
			notificationMessage = fmt.Sprintf("Book removed from the %s shelf.", listName)
		}

	}

	interrogateAPI(query)
	fmt.Println(notificationMessage) //to be shown in Alfred

}

func fetchShelves(db *sql.DB) ([]BookShelfPair, error) {
	// Start timing
	startTime := time.Now()

	query := `
	SELECT 
	s.shelf_id,
	b.book_id
	
	FROM shelf s
	LEFT JOIN books b ON b.user_book_id = s.user_book_id
	`

	// Execute the query
	rows, err := db.Query(query)

	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Slice to store (book_id, shelf_id) pairs
	var bookShelfPairs []BookShelfPair

	// Iterate over rows and store the data
	for rows.Next() {
		var pair BookShelfPair
		err := rows.Scan(&pair.ShelfID, &pair.BookID)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		bookShelfPairs = append(bookShelfPairs, pair)
	}
	// Check for errors from iterating over rows
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	// Log the execution time
	elapsedTime := time.Since(startTime)
	LogF("Execution time fetchShelves: %d ms", elapsedTime.Milliseconds())
	return bookShelfPairs, nil
}

func isBookOnShelf(pairs []BookShelfPair, bookID, shelfID int) bool {
	for _, pair := range pairs {
		if pair.BookID == bookID && pair.ShelfID == shelfID {
			return true
		}
	}
	return false
}

func getUserListBookID(db *sql.DB, bookID, shelfID int) (int, string, error) {
	var userListBookID int
	var listName string

	query := `
		SELECT s.list_book_id, s.name
		FROM shelf s
		LEFT JOIN books b ON b.user_book_id = s.user_book_id
		WHERE b.book_id = ? AND s.shelf_id = ?;
	`

	err := db.QueryRow(query, bookID, shelfID).Scan(&userListBookID, &listName)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, "", fmt.Errorf("no matching user_book_id found for bookID %d and shelfID %d", bookID, shelfID)
		}
		return 0, "", fmt.Errorf("failed to execute query: %w", err)
	}

	return userListBookID, listName, nil
}
