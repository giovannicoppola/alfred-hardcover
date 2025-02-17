package main

import (
	"os"
)

func main() {

	actionString := os.Args[1]
	// Second argument is optional
	var argString string
	if len(os.Args) >= 3 {
		argString = os.Args[2]
	} else {
		argString = ""
	}

	// LogF("username: %s", username)
	// LogF("userID: %d", userID)
	// LogF("lastUpdated: %s", lastUpdated)

	switch actionString {

	case "-build":
		{
			createLibraryDatabase()

			return
		}

	case "-library":
		{

			searchLibrary(argString)
		}

	case "-search":
		{
			SearchBookDatabase(argString)
		}

	case "-removeBook":
		{
			deleteLibraryBook()
		}

	case "-changeRating":
		{
			changeBookRating()
		}
	case "-ratings":
		{
			fetchServeRating()
		}
	case "-shelves":
		{
			fetchServeShelves()
		}
	case "-byStatus":
		{
			fetchServeStatus()
		}
	case "-toggleShelf":
		{
			toggleShelf(argString)
		}
	case "-changeStatus":
		{
			changeBookStatus()
		}
	}

}
