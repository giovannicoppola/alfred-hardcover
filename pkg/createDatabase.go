package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"time"

	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

func downloadImage(url string) error {

	if url == "" { // Skip if the URL is empty
		return nil
	}
	// Get the file name from the URL
	fileName := filepath.Base(url)
	if strings.Contains(fileName, "?") {
		// Remove query parameters if present
		fileName = strings.Split(fileName, "?")[0]
	}

	filePath := filepath.Join(coverDir, fileName)

	// Check if the file already exists
	if _, err := os.Stat(filePath); err == nil {
		// fmt.Fprintln(os.Stderr, "skipped:", filePath)
		return nil
	}
	// Download the file
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()

	// Check if the HTTP response status is OK
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: status code %d", url, resp.StatusCode)
	}

	// Create the file
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file for %s: %w", url, err)
	}
	defer file.Close()

	// Save the file
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save file for %s: %w", url, err)
	}

	fmt.Fprintln(os.Stderr, "Image saved:", filePath)
	return nil
}

func createFTSTables(db *sql.DB) error {
	// Drop and create the FTS table first
	_, err := db.Exec(`DROP TABLE IF EXISTS books_authors_fts;
	CREATE VIRTUAL TABLE books_authors_fts USING fts5(
		book_id UNINDEXED,  -- Book ID (not used for searching)
		title,              -- Book title (searchable)
		authors             -- Concatenated author names (searchable)
	);`)
	if err != nil {
		LogF("failed to create FTS table: %w", err)
	}

	// Insert data into FTS table
	_, err = db.Exec(`
		INSERT INTO books_authors_fts (book_id, title, authors)
		SELECT 
			b.book_id, 
			b.title, 
			COALESCE((
    SELECT GROUP_CONCAT(name, ', ') 
    FROM (SELECT DISTINCT name FROM author WHERE author.book_id = b.book_id)
), '') AS authors
			
			
		FROM books b
		LEFT JOIN author a ON b.book_id = a.book_id
		GROUP BY b.book_id;
	`)
	if err != nil {
		LogF("failed to populate FTS table: %w", err)
	}

	return nil
}

func createRatingstable(db *sql.DB) error {
	// first: fetch all the ratings, and count the number of ratings for each book
	// second: create the ratings table
	// third: insert the ratings into the ratings table

	query := `SELECT user_rating FROM books WHERE user_rating IS NOT NULL`
	rows, err := db.Query(query)
	if err != nil {
		LogF("failed to fetch ratings: %v", err)
		return err
	}
	defer rows.Close()

	// Initialize ratingsCount with all possible values (0, 0.5, 1, ..., 5) set to 0
	ratingsCount := make(map[float64]int)
	for rating := 0.0; rating <= 5.0; rating += 0.5 {
		ratingsCount[rating] = 0
	}

	// Count the actual ratings and NULL values
	for rows.Next() {
		var rating float64 // Allows handling NULL values
		if err := rows.Scan(&rating); err != nil {
			LogF("failed to scan row: %v", err)
			return err
		}

		if _, exists := ratingsCount[rating]; exists {
			ratingsCount[rating]++
		}

	}

	// Create the ratings table
	_, err = db.Exec(`
	DROP TABLE IF EXISTS ratings;
	CREATE TABLE  ratings (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        rating REAL,
        count INTEGER
    )`)
	if err != nil {
		LogF("failed to create ratings table: %v", err)
		return err
	}

	// Insert all rating values (even if count is 0)
	for rating, count := range ratingsCount {
		_, err := db.Exec(
			`INSERT INTO ratings (rating, count) VALUES (?, ?)`,
			rating, count,
		)
		if err != nil {
			LogF("failed to insert rating count: %v", err)
			return err
		}
	}

	return nil
}

func updateBookShelves(db *sql.DB) error {
	// Query to get user_book_id and concatenated shelves
	query := `
		SELECT b.user_book_id, 
		COALESCE((
		SELECT GROUP_CONCAT(name, ', ') 
		FROM (SELECT DISTINCT name FROM shelf WHERE shelf.user_book_id = b.user_book_id)
			), '') AS shelves


		FROM books b
		LEFT JOIN shelf s ON b.user_book_id = s.user_book_id
		GROUP BY b.user_book_id;
	`

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query shelves: %w", err)
	}
	defer rows.Close()

	// Prepare update statement
	updateStmt, err := db.Prepare("UPDATE books SET shelves = ? WHERE user_book_id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare update statement: %w", err)
	}
	defer updateStmt.Close()

	// Iterate through results and update books table
	for rows.Next() {
		var userBookID int
		var shelves string

		if err := rows.Scan(&userBookID, &shelves); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}

		// Update shelves field in books table
		_, err := updateStmt.Exec(shelves, userBookID)
		if err != nil {
			return fmt.Errorf("failed to update book: %w", err)
		}
	}

	// Check for errors in rows iteration
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}

	return nil
}

func createLibraryDatabase() ([]byte, error) {
	// A function to fetch the user's library data from the API and store it in a SQLite database.

	// Start timing
	startTime := time.Now()

	// GraphQL query for books in the user library
	query := fmt.Sprintf(`query {
  	user_books(where: {user_id: {_eq: %v}}) {
    book {
      id
      title
      rating
      cached_image
      cached_contributors
      release_year
      ratings_count
      slug
    }
    id
    status_id
    rating
    user_book_reads {
      id
      started_at
      finished_at
    }
    edition {
      isbn_10
      isbn_13
		}
	}
	}
	`, userID)

	// Fetch the user's library data
	body, err := interrogateAPI(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to fetch library:", err)
		return nil, err
	}

	var APILibrary APILibrary
	err = json.Unmarshal(body, &APILibrary)
	if err != nil {
		LogF("Error decoding library JSON response: %v", err)
		return nil, err
	}

	// 2. get the books on shelves (including those with no reading status)
	query = fmt.Sprintf(`query {

	lists(where: {user_id: {_eq: %v}}) {
		id	
		name
		books_count
		list_books {
			id
			book_id
		book {
			cached_contributors
			title
			release_year
			cached_image
			rating
			ratings_count
			slug

		}
		user_books(where: {user_id: {_eq: %v}}) {
        id
      }
    }
  }
}`, userID, userID)
	// Fetch the user's library data
	body, err = interrogateAPI(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to fetch shelves:", err)
		return nil, err
	}
	var APIshelf APIshelf
	err = json.Unmarshal(body, &APIshelf)
	if err != nil {
		LogF("Error decoding shelf JSON response: %v", err)
		return nil, err
	}

	// Open SQLite database
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		LogF("failed to open SQLite database: %v", err)
		return nil, err
	}

	_, err = db.Exec("PRAGMA journal_mode=WAL;")
	if err != nil {
		log.Fatalf("Failed to enable WAL mode: %v", err)
	}

	defer db.Close()

	// Create tables
	tableCreationQueries := []string{
		`DROP TABLE IF EXISTS books;
		CREATE TABLE books (
		book_id INTEGER PRIMARY KEY, 
		user_book_id INTEGER UNIQUE,
		user_rating REAL,
		status_id INTEGER,
		title TEXT,
		rating REAL,
		ratings_count INTEGER,
		release_year INTEGER,
		image_url TEXT,
		cover_file TEXT,
		isbn_10 TEXT,
		isbn_13 TEXT,
		slug TEXT,
		shelves TEXT
		);
		CREATE INDEX idx_books_status ON books(status_id);`,

		`DROP TABLE IF EXISTS journey;
		CREATE TABLE journey (
		journey_id INTEGER PRIMARY KEY,
		user_book_id INTEGER,
		started_at TEXT,
		finished_at TEXT,
		FOREIGN KEY(user_book_id) REFERENCES books(user_book_id)
		)`,

		`DROP TABLE IF EXISTS author;
		CREATE TABLE author (
		ID INTEGER PRIMARY KEY AUTOINCREMENT,
		book_id INTEGER,
		name TEXT,
		contribution TEXT,
		FOREIGN KEY(book_id) REFERENCES books(book_id)
		);
		CREATE INDEX idx_author_book ON author(book_id);
		`,

		`DROP TABLE IF EXISTS shelf;
		CREATE TABLE  shelf (
		ID INTEGER PRIMARY KEY AUTOINCREMENT,
		shelf_id INTEGER,
		user_book_id INTEGER,
		list_book_id INTEGER,
		name TEXT,
		FOREIGN KEY(user_book_id) REFERENCES books(user_book_id)
		);
		CREATE INDEX idx_shelf_userbook ON shelf(user_book_id);`,

		`DROP TABLE IF EXISTS bookshelves;
		CREATE TABLE bookshelves (
		shelf_id INTEGER PRIMARY KEY,
		name TEXT,
		books_count INTEGER,
		public BOOLEAN,
		slug TEXT,
		FOREIGN KEY(shelf_id) REFERENCES shelf(shelf_id)
		)`,
	}

	for _, query := range tableCreationQueries {
		if _, err := db.Exec(query); err != nil {
			log.Printf("failed to create table: %v", err)
		}
	}

	// Populating tables
	for _, userBook := range APILibrary.Data.UserBooks {
		book := userBook.Book

		// Round rating to 2 decimals
		rating := math.Round(book.Rating*100) / 100

		// Download the book cover image if not already downloaded
		downloadImage(userBook.Book.CachedImage.URL)

		// Extract cover file name from image_url
		coverFile := ""
		if userBook.Book.CachedImage.URL != "" {
			coverFile = path.Base(userBook.Book.CachedImage.URL)
		}

		// Insert into books table
		_, err := db.Exec(
			`INSERT INTO books (book_id, user_book_id, user_rating, status_id, title, rating, ratings_count, release_year, image_url, cover_file, isbn_10, isbn_13,slug)
		VALUES (?, ?, IFNULL(?, 0), ?, ?, ?, ?, ?, ?, ?, ?, ?,?)`,
			book.ID, userBook.ID, userBook.Rating, userBook.StatusID, book.Title, rating, book.RatingsCount, book.ReleaseYear, userBook.Book.CachedImage.URL, coverFile, userBook.Edition.ISBN10, userBook.Edition.ISBN13, book.Slug,
		)
		if err != nil {
			log.Printf("Failed to insert book: %v", err)
			continue
		}
		// Insert into journey table
		for _, read := range userBook.UserBookReads {
			_, err = db.Exec(
				`INSERT INTO journey (journey_id, user_book_id, started_at, finished_at)
			VALUES (?, ?, ?, ?)`,
				read.ID, userBook.ID, read.StartedAt, read.FinishedAt,
			)
			if err != nil {
				LogF("Failed to insert journey: %v", err)
			}
		}

		// Insert into author table
		for _, contribution := range book.CachedContributors {
			_, err = db.Exec(
				`INSERT INTO author (book_id, name, contribution)
			VALUES (?, ?, ?)`,
				book.ID, contribution.Author.Name, contribution.Contribution,
			)
			if err != nil {
				log.Printf("Failed to insert author: %v", err)
			}

		}
	}

	// Insert into shelf table
	shelfOnlyCounter := 0
	for _, list := range APIshelf.Data.Lists {
		for _, listBook := range list.ListBooks {
			var userBookID int
			if len(listBook.UserBooks) > 0 {
				userBookID = listBook.UserBooks[0].ID
			} else {
				shelfOnlyCounter--
				userBookID = shelfOnlyCounter
				//if userBookID is nil, then the book is not in the user's library
				// adding the book to the main library

				// Round rating to 2 decimals
				rating := math.Round(listBook.Book.Rating*100) / 100
				// Download the book cover image if not already downloaded
				downloadImage(listBook.Book.CachedImage.URL)

				// Extract cover file name from image_url
				coverFile := ""
				if listBook.Book.CachedImage.URL != "" {
					coverFile = path.Base(listBook.Book.CachedImage.URL)
				}

				_, err := db.Exec(
					`INSERT INTO books (book_id, user_book_id, user_rating, status_id, title, rating, ratings_count, release_year, image_url, cover_file, isbn_10, isbn_13,slug)
				VALUES (?,
				?,
				NULL,
				0,
				?,
				?,
				?,
				?,
				?,
				?,
				NULL,
				NULL,
				?)`,
					listBook.BookID, userBookID, listBook.Book.Title, rating, listBook.Book.RatingsCount, listBook.Book.ReleaseYear, listBook.Book.CachedImage.URL, coverFile, listBook.Book.Slug,
				)
				if err != nil {
					log.Printf("Failed to insert book without userBookID: %v", err)
					continue
				}
			}
			// Insert into author table
			for _, contribution := range listBook.Book.CachedContributors {
				_, err = db.Exec(
					`INSERT INTO author (book_id, name, contribution)
			VALUES (?, ?, ?)`,
					listBook.BookID, contribution.Author.Name, contribution.Contribution,
				)
				if err != nil {
					log.Printf("Failed to insert author: %v", err)
				}
			}
			_, err = db.Exec(
				`INSERT INTO shelf (shelf_id, user_book_id, list_book_id, name)
				VALUES (?, ?, ?,?)`,
				list.ID, userBookID, listBook.ID, list.Name,
			)
			if err != nil {
				log.Printf("Failed to insert shelf: %v", err)
			}
		}
	}

	// populate the bookshelves table
	shelves, err := fetchUserShelves()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error fetching shelves:", err)
		return nil, err
	}
	// Unmarshal the JSON data into ShelfJSON
	var shelfData ShelfJSON
	err = json.Unmarshal(shelves, &shelfData)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error unmarshalling shelves data:", err)
		return nil, err
	}
	for _, shelf := range shelfData.Data.Me[0].Lists {
		_, err = db.Exec(
			`INSERT INTO bookshelves (shelf_id, name, books_count, public, slug)
	VALUES (?, ?, ?, ?, ?)`,
			shelf.ID, shelf.Name, shelf.BooksCount, shelf.Public, shelf.Slug,
		)
		if err != nil {
			log.Printf("Failed to insert shelf: %v", err)
		}
	}

	// Call function to update shelves field
	if err := updateBookShelves(db); err != nil {
		log.Fatalf("Error updating shelves: %v", err)
	}

	createRatingstable(db)

	createFTSTables(db)
	// Create the result object
	result := map[string][]map[string]interface{}{
		"items": {map[string]interface{}{
			"title":    "Done!",
			"subtitle": "Database rebuild successful. Ready to search your library.",
			"valid":    true,
			"icon": map[string]string{
				"path": "icons/done.png",
			},
			"arg": "",
		}},
	}

	// Convert the result to JSON
	jsonResult, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		LogF("Error encoding JSON: %v", err)
		return nil, err
	}
	fmt.Println(string(jsonResult))

	// Get current time in UTC
	today := time.Now().UTC().Format("2006-01-02T15:04:05.000000-07:00")

	// Define the file name
	fileName := filepath.Join(dataFolder, "lastUpdatedLocal")

	// Write the formatted timestamp to the file
	err = os.WriteFile(fileName, []byte(today), 0644)
	if err != nil {
		fmt.Println("Failed to save date to file:", err)
	}

	elapsedTime := time.Since(startTime)
	LogF("Database rebuild execution time: %d ms", elapsedTime.Milliseconds())
	os.Exit(0)
	return nil, nil
}
