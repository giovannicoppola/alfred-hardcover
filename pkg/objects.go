package main

import (
	_ "github.com/mattn/go-sqlite3"
)

// GraphQLRequest defines the structure of a GraphQL query
type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// GraphQLResponse captures the response from the API
type GraphQLResponse struct {
	Data   interface{} `json:"data"`
	Errors interface{} `json:"errors"`
}

type BookSearch struct {
	Found       int     `json:"found"`
	Authors     string  `json:"authors"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	ImageURL    string  `json:"image_url"`
	Rating      float64 `json:"rating"`
	Raters      int     `json:"ratings_count"`
	ID          int     `json:"id"`
	ReleaseYear int     `json:"release_year"`
	Slug        string  `json:"slug"`
}

type BookInfoMap struct {
	StatusID   int
	UserRating float64
	Shelves    string
}

type GraphQLResponseSearch struct {
	Data struct {
		Search struct {
			Results struct {
				Found int `json:"found"`
				Hits  []struct {
					Document struct {
						Title       string `json:"title"`
						Description string `json:"description"`
						Slug        string `json:"slug"`
						Image       struct {
							URL string `json:"url"`
						} `json:"image"`
						Rating        float64 `json:"rating"`
						Raters        int     `json:"ratings_count"`
						ID            string  `json:"id"`
						ReleaseYear   int     `json:"release_year"`
						Contributions []struct {
							Author struct {
								Name string `json:"name"`
							} `json:"author"`
						} `json:"contributions"`
					} `json:"document"`
				} `json:"hits"`
			} `json:"results"`
		} `json:"search"`
	} `json:"data"`
}

type ShelfJSON struct {
	Data struct {
		Me []struct {
			Lists []struct {
				Name       string `json:"name"`
				ID         int    `json:"id"`
				BooksCount int    `json:"books_count"`
				Public     bool   `json:"public"`
				Slug       string `json:"slug"`
			} `json:"lists"`
		} `json:"me"`
	} `json:"data"`
}

type BookShelfPair struct {
	BookID  int
	ShelfID int
}

var ReadStatus = map[int]string{
	1: "toRead",
	2: "Reading",
	3: "Read",
	4: "DNF",
}

var ReadStatusKeys = []int{1, 2, 3, 4}

var ReadStatusCount = map[int]int{
	1: 0,
	2: 0,
	3: 0,
	4: 0,
}

var ReadStatusIcon = map[int]string{
	1: "icons/bookPile.png",
	2: "icons/open-book.png",
	3: "icons/shelf.png",
	4: "icons/bookOld.png",
}
var RatingEmoji = map[float64]string{
	0.5: "✨️",
	1:   "⭐️",
	1.5: "⭐️✨️",
	2:   "⭐️⭐️",
	2.5: "⭐️⭐️✨️",
	3:   "⭐️⭐️⭐️",
	3.5: "⭐️⭐️⭐️✨️",
	4:   "⭐️⭐️⭐️⭐️",
	4.5: "⭐️⭐️⭐️⭐️✨️",
	5:   "⭐️⭐️⭐️⭐️⭐️",
}
var orderClauses = map[string]string{
	"--y": " ORDER BY b.release_year DESC",
	"--r": " ORDER BY b.rating DESC",
	"--t": " ORDER BY b.title ASC",
}
var ReadStatusEmoji = map[int]string{
	1: "📚️",
	2: "📖",
	3: "✅️",
	4: "📕",
}

type APILibrary struct {
	Data struct {
		UserBooks []UserBook `json:"user_books"`
	} `json:"data"`
}

type UserBook struct {
	ID            int            `json:"id"`
	StatusID      int            `json:"status_id"`
	Rating        *float64       `json:"rating"`
	UserBookReads []UserBookRead `json:"user_book_reads"`
	Book          Book           `json:"book"`
	Edition       Edition        `json:"edition"`
}

type Book struct {
	ID                 int                `json:"id"`
	Title              string             `json:"title"`
	Rating             float64            `json:"rating"`
	ReleaseYear        int                `json:"release_year"`
	RatingsCount       int                `json:"ratings_count"`
	Slug               string             `json:"slug"`
	CachedImage        CachedImage        `json:"cached_image"`
	CachedContributors []ContributorEntry `json:"cached_contributors"`
}

type CachedImage struct {
	ID        int    `json:"id"`
	URL       string `json:"url"`
	Color     string `json:"color"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	ColorName string `json:"color_name"`
}

type ContributorEntry struct {
	Author       Author  `json:"author"`
	Contribution *string `json:"contribution"`
}

type Author struct {
	Slug        string      `json:"slug"`
	Name        string      `json:"name"`
	CachedImage CachedImage `json:"cachedImage"`
}

type UserBookRead struct {
	ID         int     `json:"id"`
	StartedAt  *string `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
}

type Edition struct {
	ISBN10 *string `json:"isbn_10"`
	ISBN13 *string `json:"isbn_13"`
}

type APIshelf struct {
	Data struct {
		Lists []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			BooksCount int    `json:"books_count"`
			ListBooks  []struct {
				ID     int `json:"id"`
				BookID int `json:"book_id"`
				Book   struct {
					Title              string      `json:"title"`
					ReleaseYear        int         `json:"release_year"`
					CachedImage        CachedImage `json:"cached_image"`
					Rating             float64     `json:"rating"`
					Slug               string      `json:"slug"`
					RatingsCount       int         `json:"ratings_count"`
					CachedContributors []struct {
						Author struct {
							Slug        string `json:"slug"`
							Name        string `json:"name"`
							CachedImage struct {
								ID        int    `json:"id"`
								URL       string `json:"url"`
								Color     string `json:"color"`
								Width     int    `json:"width"`
								Height    int    `json:"height"`
								ColorName string `json:"color_name"`
							} `json:"cachedImage"`
						} `json:"author"`
						Contribution *string `json:"contribution"` // Nullable field
					} `json:"cached_contributors"`
				} `json:"book"`
				UserBooks []struct {
					ID int `json:"id"`
				} `json:"user_books"`
			} `json:"list_books"`
		} `json:"lists"`
	} `json:"data"`
}
