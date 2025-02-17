package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func interrogateAPI(myQuery string) ([]byte, error) {

	// Start timing
	startTime := time.Now()

	if authToken == "" {
		err := fmt.Errorf("error: Authorization token is empty")
		fmt.Fprintln(os.Stderr, err) // Print error to stderr
		return nil, err
	}

	// Build the request payload
	requestBody := GraphQLRequest{
		Query: myQuery,
	}

	payload, err := json.Marshal(requestBody)
	if err != nil {
		err = fmt.Errorf("error encoding JSON: %w", err)
		fmt.Fprintln(os.Stderr, err) // Print error to stderr
		return nil, err
	}

	// Make the HTTP request
	client := &http.Client{}
	req, err := http.NewRequest("POST", apiRoot, bytes.NewBuffer(payload))
	if err != nil {
		err = fmt.Errorf("error creating HTTP request: %w", err)
		fmt.Fprintln(os.Stderr, err) // Print error to stderr
		return nil, err
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authToken)

	// Execute the HTTP request

	resp, err := client.Do(req)
	if err != nil {
		err = fmt.Errorf("error making HTTP request: %w", err)
		fmt.Fprintln(os.Stderr, err) // Print error to stderr
		return nil, err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("error reading response body: %w", err)
		fmt.Fprintln(os.Stderr, err) // Print error to stderr
		return nil, err

	}

	// Log the execution time
	elapsedTime := time.Since(startTime)
	LogF("Execution time (interrogateAPI): %d ms", elapsedTime.Milliseconds())

	return body, nil
}

func filterLibrarySearch(query string) ([]string, bool, []string, string) {
	// Split the query into words (tokens)
	tokens := strings.Fields(query)
	var STATUS_FLAG bool
	var TAG_FRAG string
	// Create a slice to store the filtered tokens
	var filteredTokens []string

	// Iterate over tokens and parse the search query
	for _, token := range tokens {
		if strings.HasPrefix(token, "@") {

			STATUS_FLAG = true
			// Removing '@' from the beginning of the string
			cleanedInput := strings.TrimPrefix(token, "@")
			TAG_FRAG = cleanedInput

			// Check if it matches any value in the ReadStatus map
			for key, status := range ReadStatus {
				if cleanedInput == status {
					whereClauses = append(whereClauses, fmt.Sprintf("b.status_id =%v ", key))
					STATUS_FLAG = false
					// filteredTokens = append(filteredTokens, token)
				}
			}

			continue

		}
		filteredTokens = append(filteredTokens, token)
	}
	return filteredTokens, STATUS_FLAG, whereClauses, TAG_FRAG
}

func searchLibrary(searchString string) ([]byte, error) {
	// Start timing
	startTime := time.Now()
	var TAG_FRAG string

	//get the breadCrumb environment variable
	breadCrumb := os.Getenv("breadCrumb")
	var terms []string
	var STATUS_FLAG bool

	var orderClause string
	var backString string

	// this should be integrated in the filterLibrarySearch function
	for key := range orderClauses { // Loop through map keys
		if strings.Contains(searchString, key) {
			searchString = strings.ReplaceAll(searchString, key, "")
			orderClause = orderClauses[key]
			break // Stop after the first match
		}
	}
	if searchString != "" {
		terms, STATUS_FLAG, whereClauses, TAG_FRAG = filterLibrarySearch(searchString)
	}

	switch breadCrumb {
	case "listShelfBooks":
		currentShelfID := os.Getenv("current_listID")
		// LogF("currentShelfID: %v", currentShelfID)
		whereClauses = append(whereClauses, fmt.Sprintf("s.shelf_id = %v ", currentShelfID))
		backString = "⬅️ back to shelves"

	case "listStatusBooks":
		current_StatusID, err := strconv.Atoi(os.Getenv("newStatus"))
		backString = "⬅️ back to reading status"
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error converting current_statusID: %s", err)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("b.status_id = %v ", current_StatusID))
		// whereClauseCrumb = fmt.Sprintf(" WHERE b.status_id = %v ", current_StatusID)

	case "listRatings":
		currentRating, err := strconv.ParseFloat(os.Getenv("newRating"), 64)
		backString = "⬅️ back to ratings"
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error converting current_rating: %s", err)
		}
		whereClauses = append(whereClauses, fmt.Sprintf("b.user_rating = %.1f ", currentRating))

	}
	// Open SQLite database
	db, err := sql.Open("sqlite3", databasePath)

	if err != nil {
		fmt.Fprintf(os.Stdout, "failed to open SQLite database: %v\n", err)
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}
	defer db.Close()
	// SQL query and arguments
	var query string
	var args []interface{}

	query = `
		SELECT 
			b.book_id,	
			b.title,
			f.authors,
			b.release_year,
			b.user_rating,
			b.rating,
			b.ratings_count,
			shelves,
			b.cover_file,
			b.status_id,
			b.user_book_id,
			b.slug,
			COUNT(*) OVER () AS total_count,	
			COUNT(*) FILTER (WHERE b.status_id = 1) OVER () AS count_status_1,
			COUNT(*) FILTER (WHERE b.status_id = 2) OVER () AS count_status_2,
			COUNT(*) FILTER (WHERE b.status_id = 3) OVER () AS count_status_3,
			COUNT(*) FILTER (WHERE b.status_id = 4) OVER () AS count_status_4
			

			FROM books_authors_fts f
			JOIN books b ON f.book_id = b.book_id 
			
			LEFT JOIN shelf s ON b.user_book_id = s.user_book_id	
		
	`
	// Append '*' to each term
	if len(terms) > 0 {
		var searchTerms []string
		for _, term := range terms {
			searchTerms = append(searchTerms, term+"*")
		}
		// Join terms with space
		searchPattern := strings.Join(searchTerms, " ")
		whereClauses = append(whereClauses, "(books_authors_fts MATCH ?)")
		args = append(args, searchPattern)

	}
	// Combine the WHERE clause with OR conditions
	if len(whereClauses) > 0 {
		query += `
			
			WHERE ` + strings.Join(whereClauses, " AND ")
	}

	// Add GROUP BY clause
	query += " GROUP BY b.book_id"

	// Add ORDER BY clause
	if orderClause != "" {
		query += orderClause
	}

	// LogF("Query: %s", query)

	// Execute the query
	rows, err := db.Query(query, args...)
	if err != nil {
		fmt.Fprintln(os.Stdout, "failed to execute query:", err) // Print to stdout
		return nil, fmt.Errorf("failed to execute query: %w", err)

	}
	defer rows.Close()

	var resultCount int
	bookCount := 0
	nonZeroResults := false

	// Create the result object
	result := map[string][]map[string]interface{}{
		"items": {},
	}

	// LogF("current terms: %s", strings.Join(terms, " "))

	p := message.NewPrinter(language.English) // this is to add the thousand separator
	firstRow := true
	// Iterate through the rows
	for rows.Next() {
		nonZeroResults = true
		var title, authors, shelves, coverFile, slug string
		var user_rating, rating sql.NullFloat64
		var statusID, book_id, user_book_id, release_year, ratings_count, statusCount1, statusCount2, statusCount3, statusCount4 int
		bookCount++

		err := rows.Scan(&book_id, &title, &authors, &release_year, &user_rating, &rating, &ratings_count, &shelves, &coverFile, &statusID, &user_book_id, &slug, &resultCount, &statusCount1, &statusCount2, &statusCount3, &statusCount4)
		if err != nil {
			LogF("failed to scan row: %v", err)
			continue
		}

		// Now assign the scanned values to the map
		ReadStatusCount[1] = statusCount1
		ReadStatusCount[2] = statusCount2
		ReadStatusCount[3] = statusCount3
		ReadStatusCount[4] = statusCount4

		// hijack the result list to display the status list and count if needed
		if firstRow && STATUS_FLAG {

			// Create the result object
			statusList := map[string][]map[string]interface{}{}
			for _, key := range ReadStatusKeys {
				myProcessedSearchString := strings.Join(terms, " ") + " @" + ReadStatus[key]
				if strings.Contains(strings.ToLower(ReadStatus[key]), strings.ToLower(TAG_FRAG)) {

					statusList["items"] = append(statusList["items"], map[string]interface{}{
						"title":    fmt.Sprintf("%s (%d)", ReadStatus[key], ReadStatusCount[key]),
						"subtitle": "Filter by reading status",
						"valid":    true,
						"variables": map[string]interface{}{
							"searchSource":          "statusSearch",
							"processedSearchString": myProcessedSearchString,
						},
						"icon": map[string]string{
							"path": ReadStatusIcon[key],
						},
						"arg": myProcessedSearchString + " ",
					})
				} else {
					statusList["items"] = append(result["items"], map[string]interface{}{
						"title":    "unknown tag 🙂",
						"subtitle": "review please!",
						"valid":    true,
						"icon": map[string]string{
							"path": "icons/hopeless.png",
						},
					})
				}
			}
			// Convert the result to JSON
			jsonData, err := json.MarshalIndent(statusList, "", "  ")
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to encode JSON: %s", err)
			}
			fmt.Println(string(jsonData))
			os.Exit(0)

			firstRow = false
		} else {
			firstRow = false
		}

		// Handle nullable fields
		ratingStr := ""
		ratingSubtitle := "Rate this book"
		if user_rating.Valid && user_rating.Float64 > 0 {

			ratingStr = fmt.Sprintf("%.1f⭐️", user_rating.Float64)
			ratingSubtitle = "Change rating (currently: " + ratingStr + ")"
		}

		overallRating := p.Sprintf("%.2f☆%d", rating.Float64, ratings_count)

		shelveSubtitle := "Add to shelf"
		shelveStatus := ""
		if shelves != "" {
			shelveStatus = "(currently: " + shelves + ")"
			shelveSubtitle = "Add/remove from shelves " + shelveStatus
		}

		var readingSubtitle, delSubtitle string
		switch statusID {
		case 1, 2, 3, 4:

			readingSubtitle = "Change reading status (currently: " + ReadStatus[statusID] + ReadStatusEmoji[statusID] + ")"
			delSubtitle = "Remove this book from your library 🚮"
		case 0:
			readingSubtitle = "Change reading status"

		}
		// Convert release_year to string without formatting
		releaseYearStr := fmt.Sprintf("%d", release_year)
		// Append data to the result
		result["items"] = append(result["items"], map[string]interface{}{
			"title":    title + " " + ReadStatusEmoji[statusID],
			"subtitle": p.Sprintf("%d/%d %s (%s) %s (%s)", bookCount, resultCount, authors, releaseYearStr, ratingStr, overallRating),
			"valid":    true,
			"icon": map[string]string{
				"path": filepath.Join(coverDir, coverFile),
			},

			"mods": map[string]interface{}{

				"cmd": map[string]interface{}{
					"valid":    true,
					"arg":      "",
					"subtitle": readingSubtitle,
					"variables": map[string]interface{}{
						"current_user_bookID": user_book_id,
						"current_statusID":    statusID,
						"current_bookID":      book_id,
						"mySearchString":      searchString,
					},
				},
				"ctrl": map[string]interface{}{
					"valid":    true,
					"arg":      "",
					"subtitle": shelveSubtitle,
					"variables": map[string]interface{}{
						"current_bookID": book_id,
						"mySearchString": searchString,
					},
				},
				"alt": map[string]interface{}{
					"valid":    true,
					"arg":      "",
					"subtitle": ratingSubtitle,
					"variables": map[string]interface{}{
						"current_bookID": book_id,
						"current_rating": user_rating.Float64,
						"mySearchString": searchString,
					},
				},
				"ctrl+cmd": map[string]interface{}{
					"subtitle": delSubtitle,
					"valid":    true,
					"arg":      "-removeBook",
					"variables": map[string]interface{}{

						"current_user_bookID": user_book_id,
					},
				},
				"cmd+alt": map[string]interface{}{
					"subtitle": backString,
					"valid":    true,
					"arg":      "",
					"variables": map[string]interface{}{
						"mySearchString": searchString,
					},
				},
			},
			"arg": baseURL + slug,
			"variables": map[string]interface{}{
				"searchSource": "",
			},
		})

	}

	// Check for errors after iteration
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error during rows iteration: %w", err)
	}
	// Check if there are no results
	if !nonZeroResults {
		result["items"] = append(result["items"], map[string]interface{}{
			"title":    "no results here 🙂",
			"subtitle": "try something else",
			"valid":    true,
			"icon": map[string]string{
				"path": "icons/hopeless.png",
			},
		})
	}

	// Convert the result to JSON
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode JSON: %w", err)
	}
	// Calculate and log execution time
	elapsedTime := time.Since(startTime)
	LogF("Execution time (library search): %d ms", elapsedTime.Milliseconds())
	fmt.Println(string(jsonData))
	return jsonData, nil
}

func fetchBookIDs() (map[int]BookInfoMap, error) {
	// Start timing
	startTime := time.Now()

	// Open SQLite database
	db, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		LogF("ERROR")
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}
	defer db.Close()

	// SQL query to retrieve book_id and status_id
	query := `
	SELECT 
		book_id,
		status_id,
		user_rating,
		COALESCE(GROUP_CONCAT(DISTINCT s.name) , '') AS shelves
	FROM books b
	LEFT JOIN shelf s ON b.user_book_id = s.user_book_id
	GROUP BY b.book_id
	`

	// Execute the query
	rows, err := db.Query(query)

	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Map to store book_id and status_id
	bookStatusMap := make(map[int]BookInfoMap)

	// Iterate over rows and store the data in the map
	for rows.Next() {
		var bookID int
		var shelves string
		var userRating sql.NullFloat64
		var statusID sql.NullInt64 // Handle NULL values

		err := rows.Scan(&bookID, &statusID, &userRating, &shelves)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		if statusID.Valid || userRating.Valid {
			bookStatusMap[bookID] = struct {
				StatusID   int
				UserRating float64
				Shelves    string
			}{
				StatusID:   int(statusID.Int64), // Convert SQL NULL to int
				UserRating: userRating.Float64,  // Convert SQL NULL to float64
				Shelves:    shelves,
			}
		} else {
			bookStatusMap[bookID] = struct {
				StatusID   int
				UserRating float64
				Shelves    string
			}{
				StatusID:   0, // Default if NULL
				UserRating: 0, // Default if NULL
				Shelves:    shelves,
			}
		}
	}

	// Check for errors from iterating over rows
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	// Log the execution time
	elapsedTime := time.Since(startTime)
	LogF("Execution time (book ID fetching): %d ms", elapsedTime.Milliseconds())

	return bookStatusMap, nil
}

func deleteLibraryBook() {

	// get the user_book_id environment variable

	var bookIDInt int
	if os.Getenv("current_user_bookID") != "" {
		//convert bookID to int
		var err error
		bookIDInt, err = strconv.Atoi(os.Getenv("current_user_bookID"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error converting bookID: %s", err)
		}

	} else {
		fmt.Fprintf(os.Stderr, "No user_bookID or bookID found")
	}

	query := fmt.Sprintf(`mutation {
		delete_user_book(id: %v) {
		  book_id
		}
		}`, bookIDInt)
	interrogateAPI(query)
	notificationString := "Book eliminated from library 🚮"
	fmt.Println(notificationString)
}

/*
QUERY IF I NEED TO SUMMARIZE A CATEGORY WITH MULTIPLE VALUES
SELECT
    b.book_id,
    b.title,
    COALESCE(GROUP_CONCAT(DISTINCT a.name), '') AS authors,
    b.user_rating,
    b.rating,
    b.ratings_count,
    COALESCE(GROUP_CONCAT(DISTINCT s.name), '') AS shelves,
    b.cover_file,
    b.status_id,
    b.user_book_id,
    b.slug,
    COUNT(*) OVER () AS total_count,

    -- Join aggregated status counts
    status_counts.status_id,
    status_counts.count_per_status

FROM books b
LEFT JOIN author a ON b.book_id = a.book_id
LEFT JOIN shelf s ON b.user_book_id = s.user_book_id
LEFT JOIN (
    SELECT status_id, COUNT(*) AS count_per_status
    FROM books
    GROUP BY status_id
) AS status_counts ON b.status_id = status_counts.status_id

%s
GROUP BY b.book_id
%s;


*/
