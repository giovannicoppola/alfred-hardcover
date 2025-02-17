package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

/*
	func fetchImages(imageURLs []string) error {
		// Get the system temporary directory
		tempDir := os.TempDir()

		for _, url := range imageURLs {
			if url == "" { // Skip if the URL is empty
				continue
			}
			// Get the file name from the URL
			fileName := filepath.Base(url)
			if strings.Contains(fileName, "?") {
				// Remove query parameters if present
				fileName = strings.Split(fileName, "?")[0]
			}
			filePath := filepath.Join(tempDir, fileName)

			// Download the file
			resp, err := http.Get(url)
			if err != nil {
				LogF("Failed to download %s: %v\n", url, err)
				continue
			}
			defer resp.Body.Close()

			// Check if the HTTP response status is OK
			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Failed to download %s: status code %d\n", url, resp.StatusCode)
				continue
			}

			// Create the file in the temp directory
			file, err := os.Create(filePath)
			if err != nil {
				fmt.Printf("Failed to create file for %s: %v\n", url, err)
				continue
			}
			defer file.Close()

			// Save the file
			_, err = io.Copy(file, resp.Body)
			if err != nil {
				fmt.Printf("Failed to save file for %s: %v\n", url, err)
			} else {
				// LogF("Image saved: %s\n", filePath)
			}
		}

		return nil
	}
*/
func fetchImages(imageURLs []string) error {
	var wg sync.WaitGroup
	tempDir := os.TempDir()
	semaphore := make(chan struct{}, 5) // Limit to 5 concurrent downloads

	for _, url := range imageURLs {
		if url == "" {
			continue
		}

		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire a slot
			defer func() { <-semaphore }() // Release slot

			// Get the file name
			fileName := filepath.Base(url)
			if strings.Contains(fileName, "?") {
				fileName = strings.Split(fileName, "?")[0]
			}
			filePath := filepath.Join(tempDir, fileName)

			// Download the file
			resp, err := http.Get(url)
			if err != nil {
				LogF("Failed to download %s: %v\n", url, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				LogF("Failed to download %s: status code %d\n", url, resp.StatusCode)
				return
			}

			// Create the file
			file, err := os.Create(filePath)
			if err != nil {
				LogF("Failed to create file for %s: %v\n", url, err)
				return
			}
			defer file.Close()

			// Save the file
			_, err = io.Copy(file, resp.Body)
			if err != nil {
				LogF("Failed to save file for %s: %v\n", url, err)
			}

		}(url)
	}

	wg.Wait() // Wait for all downloads to complete
	return nil
}

func extractBooks(responseBody []byte) ([]BookSearch, error) {
	var graphqlResponse GraphQLResponseSearch
	err := json.Unmarshal(responseBody, &graphqlResponse)
	if err != nil {
		LogF("Error decoding JSON response:", err)
		return nil, err
	}

	var books []BookSearch
	results := graphqlResponse.Data.Search.Results
	for _, hit := range results.Hits {
		doc := hit.Document
		var authorNames []string
		for _, contribution := range doc.Contributions {
			authorNames = append(authorNames, contribution.Author.Name)
		}

		// Converting doc.ID to int
		id, err := strconv.Atoi(doc.ID) // Convert doc.ID from string to int
		if err != nil {
			LogF("Error converting doc.ID to int: %v", err)

		}
		book := BookSearch{
			Found:       results.Found,
			Authors:     strings.Join(authorNames, ", "),
			Title:       doc.Title,
			Description: doc.Description,
			ImageURL:    doc.Image.URL,
			Rating:      doc.Rating,
			Raters:      doc.Raters,
			ID:          id,
			ReleaseYear: doc.ReleaseYear,
			Slug:        doc.Slug,
		}
		books = append(books, book)
	}

	return books, nil
}

func loadCachedSearchResults() []BookSearch {
	startTime := time.Now()
	var book []BookSearch
	err := json.Unmarshal([]byte(os.Getenv("BOOK_SEARCH")), &book)
	if err != nil {
		fmt.Println("Error unmarshalling:", err)
		return book
	}
	LogF("Book list loaded from environment")
	elapsedTime := time.Since(startTime)
	LogF("Execution time loading cached search results: %d ms", elapsedTime.Milliseconds())
	return book

}

func filterSearchQuery(query string) string {
	// Split the query into words (tokens)
	tokens := strings.Fields(query)

	// Create a slice to store the filtered tokens
	var filteredTokens []string

	// Iterate over tokens and filter out unwanted elements
	for _, token := range tokens {
		if strings.HasPrefix(token, "@") || token == "-" || token == "--" {
			continue // Skip tokens that start with '@' or are '-'/'--' alone
		}
		filteredTokens = append(filteredTokens, token)
	}
	currentDBsearch := strings.Join(filteredTokens, " ")
	// currentDBsearch = strings.ReplaceAll(currentDBsearch, " ", ".")
	LogF("Current search: %s", currentDBsearch)
	// Set the current search as an environment variable
	// Create JSON response with only variables
	return currentDBsearch
}

func queryRemoteDatabase(searchString string) []BookSearch {
	startTime := time.Now()

	// Define the corrected GraphQL query, replacing "james monroe" with searchString
	query := fmt.Sprintf(`{search(query: "%s", query_type: "Book", per_page: %d, page: 1) {
		results  
		  }}`, searchString, numberResults)

	// Build the request payload
	requestBody := GraphQLRequest{
		Query: query,
	}

	payload, err := json.Marshal(requestBody)
	if err != nil {
		LogF("Error encoding JSON:", err)
		return []BookSearch{}
	}
	// LogF("Request Payload:", string(payload))

	// Make the HTTP request
	client := &http.Client{}
	req, err := http.NewRequest("POST", apiRoot, bytes.NewBuffer(payload))
	if err != nil {
		LogF("Error creating HTTP request:", err)
		return []BookSearch{}
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authToken)

	// Execute the HTTP request
	LogF("interrogating the API...")
	resp, err := client.Do(req)
	if err != nil {
		LogF("Error making HTTP request:", err)
		return []BookSearch{}
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	// LogF("Response body:", string(body))
	if err != nil {
		LogF("Error reading response body:", err)
		return []BookSearch{}
	}
	books, err := extractBooks(body) // `body` is the HTTP response body
	if err != nil {
		LogF("Error extracting books:", err)
		return []BookSearch{}
	}

	elapsedTime := time.Since(startTime)
	LogF("Execution time remote database search: %d ms", elapsedTime.Milliseconds())
	return books
}

func SearchBookDatabase(searchString string) {
	var books []BookSearch
	// Start timing
	startTime := time.Now()
	// Validate the token format

	// Filter the search query
	previousSearch := os.Getenv("CURRENT_DB_SEARCH")
	// previousSearch = strings.ReplaceAll(previousSearch, " ", ".")
	LogF("Previous search: %s", previousSearch)
	var sortFlag string
	for key := range orderClauses {
		if strings.Contains(searchString, key) {
			// Remove the sorting flag from searchString
			searchString = strings.ReplaceAll(searchString, key, "")
			sortFlag = key
			break
		}
	}
	searchString = filterSearchQuery(searchString)
	if searchString == previousSearch {
		LogF("Search string is the same as the previous search")
		books = loadCachedSearchResults()
	} else {
		books = queryRemoteDatabase(searchString)
		// Collect all image URLs
		var imageURLs []string
		for _, book := range books {
			if book.ImageURL != "" {
				imageURLs = append(imageURLs, book.ImageURL)
			}
		}

		// Call fetchImages once with the complete list
		fetchImages(imageURLs)

	}

	if sortFlag != "" {
		LogF(fmt.Sprintf("AHA there is a sortflag: %s", sortFlag))

		// Iterate over orderClauses keys
		// Apply sorting based on the matched key
		switch sortFlag {
		case "--y":
			sort.Slice(books, func(i, j int) bool {
				return books[i].ReleaseYear > books[j].ReleaseYear // Descending
			})
		case "--r":
			sort.Slice(books, func(i, j int) bool {
				return books[i].Rating > books[j].Rating // Descending
			})
		case "--t":
			sort.Slice(books, func(i, j int) bool {
				return books[i].Title < books[j].Title // Ascending
			})
		}
	}
	result := make(map[string]interface{})
	// Assign "cache" as a nested map
	// result["cache"] = map[string]interface{}{
	// 	"seconds": 3600,
	// }
	// result["rerun"] = 1 // it seems there is no need to rerun

	result["items"] = []map[string]interface{}{}

	bookStatusMap, err := fetchBookIDs()

	if err != nil {
		fmt.Println("Error fetching book IDs:", err)
		return
	}
	elapsedTime := time.Since(startTime)
	LogF("Execution time before serializing: %d ms", elapsedTime.Milliseconds())
	// Serialize the struct to JSON
	bookJSON, err := json.Marshal(books)
	if err != nil {
		LogF("Error marshalling struct:", err)
		return
	}

	bookTotal := len(books)
	bookCount := 0
	elapsedTime = time.Since(startTime)

	LogF("Execution time before starting loop: %d ms", elapsedTime.Milliseconds())
	for _, book := range books {
		var currentRating float64
		bookCount++
		// Format rating as string
		ratingStr := fmt.Sprintf("%.2f", book.Rating)
		// fetchImages([]string{book.ImageURL})
		// Extract cover file name from image_url
		coverFile := ""
		if book.ImageURL != "" {
			coverFile = path.Base(book.ImageURL)
		}
		// Check if book_id exists in the map and compare the status_id
		var userLibrarySymbol string
		readingStatusSubtitle := "Assign reading status"
		ratingSubtitle := "Assign rating"
		var shelfSubtitle string
		var shelfSymbol string
		if bookInfo, exists := bookStatusMap[book.ID]; exists {
			readingStatusSubtitle = "Change reading status"
			userLibrarySymbol = ReadStatusEmoji[bookInfo.StatusID]
			switch bookInfo.StatusID {
			case 1:
				readingStatusSubtitle = "Change reading status (currently: to read)"
			case 2:
				readingStatusSubtitle = "Change reading status (currently: reading)"
			case 3: // Multiple cases can share the same block
				readingStatusSubtitle = "Change reading status (currently: read)"
			case 4: // Multiple cases can share the same block
				readingStatusSubtitle = "Change reading status (currently: DNF)"
			default:
				readingStatusSubtitle = "Assign reading status"
			}
			if bookInfo.UserRating > 0 {
				currentRating = bookInfo.UserRating
				ratingSubtitle = fmt.Sprintf("Change rating (currently: %.1f⭐️)", currentRating)
			} else {
				ratingSubtitle = "Assign rating"
			}

			if bookInfo.Shelves != "" {
				shelfSubtitle = fmt.Sprintf("Add/remove from shelves (currently: %s)", bookInfo.Shelves)
				shelfSymbol = " 🏷️"
			} else {
				shelfSubtitle = "Add to shelf"
				shelfSymbol = ""
			}
		}
		p := message.NewPrinter(language.English)
		if ratingStr == "0.00" {
			ratingStr = ""
		} else {
			if currentRating > 0 {
				ratingStr = p.Sprintf("%.1f⭐️ %s☆%d", currentRating, ratingStr, book.Raters)
			} else {
				ratingStr = p.Sprintf("%s☆%d", ratingStr, book.Raters)
			}
		}

		// Append formatted data to the result
		result["items"] = append(result["items"].([]map[string]interface{}), map[string]interface{}{
			"title":    book.Title + " " + userLibrarySymbol + shelfSymbol,
			"subtitle": fmt.Sprintf("%v/%v, %s (%v) %s", bookCount, bookTotal, book.Authors, book.ReleaseYear, ratingStr),
			"valid":    true,
			"icon": map[string]string{
				"path": filepath.Join(tempDir, coverFile),
			},
			"mods": map[string]interface{}{
				"cmd": map[string]interface{}{
					"subtitle": readingStatusSubtitle,
					"valid":    true,
					"variables": map[string]interface{}{ // Additional metadata
						"current_bookID": book.ID,
					},
					"arg": "",
					//"subtitle": book.Description, // Or another field as needed
				},

				"alt": map[string]interface{}{
					"valid":    true,
					"arg":      "",
					"subtitle": ratingSubtitle,
					"variables": map[string]interface{}{
						"current_bookID": book.ID,
						"current_rating": currentRating,
					},
				},
				"ctrl": map[string]interface{}{
					"valid":    true,
					"arg":      "",
					"subtitle": shelfSubtitle,
					"variables": map[string]interface{}{
						"current_bookID": book.ID,
					},
				},
			},
			"arg": baseURL + book.Slug,
			"variables": map[string]interface{}{ // Additional metadata
				"current_bookID": book.ID,
				"release_year":   book.ReleaseYear,
			},
		})
		result["variables"] = map[string]interface{}{

			"CURRENT_DB_SEARCH": searchString,
			"BOOK_SEARCH":       string(bookJSON),
		}
	}
	elapsedTime = time.Since(startTime)
	LogF("Execution time after loop: %d ms", elapsedTime.Milliseconds())
	// Convert to JSON
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		LogF("Error encoding JSON:", err)
		return
	}

	fmt.Println(string(jsonData))

	// Log the execution time
	elapsedTime = time.Since(startTime)
	LogF("Execution time book catalog search: %d ms", elapsedTime.Milliseconds())
}
