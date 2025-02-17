# alfred-hardcover 📘
### An Alfred workflow to interact with [Hardcover](https://hardcover.app/), a modern book tracking and community of book lovers.  


<a href="https://github.com/giovannicoppola/alfred-hardcover/releases/latest/">
<img alt="Downloads"
src="https://img.shields.io/github/downloads/giovannicoppola/alfred-hardcover/total?color=purple&label=Downloads"><br/>
</a>

![](images/alfred-hardcover.gif)


<!-- MarkdownTOC autolink="true" bracket="round" depth="3" autoanchor="true" -->

- [Motivation](#motivation)
- [Setting up](#setting-up)
- [Basic Usage](#usage)
- [Known Issues](#known-issues)
- [Changelog](#changelog)
- [Roadmap](#roadmap)
- [Acknowledgments](#acknowledgments)
- [Feedback](#feedback)

<!-- /MarkdownTOC -->


<h1 id="motivation">Motivation ✅</h1>
Being able to quickly:
- add a book to my library
- check my library for the status, rating, and shelves of a book, and change them
- find a book, or shelf, and open it on Hardcover
- list my books by status, shelf, or rating.

<h1 id="setting-up">Setting up ⚙️</h1>

- get your API key [here](https://hardcover.app/account/api), and paste it into the `Hardcover API token` field in `Workflow Configuration`

## Options
- set the number of results when querying the Hardcover database (slightly slower if many). Default: 19.
- set the interval at which `alfred-hardcover` checks for changes on the main Hardcover site (default: 7). Database is automatically refreshed after any changes are made by `alfred-hardcover`.

<h1 id="usage">Basic Usage 📖</h1>
The fundamental unit of the Workflow is a book result. Once you get to a list of books you can perform one of these operations:

1. add or remove from shelves (`^ (ctrl)`)
2. change reading status (`⌘ (cmd)`)
3. open on Hardcover (`↩️ (enter)`)
4. assign or change rating (`⌥ (option)`)
5. delete from library (`⌘-^ (cmd-ctrl)`)
6. Quick Look (`⇧ shift`) will load the corresponding Hardcover page.  

How to get to a list of books? Five main ways:
1. by listing the books in your library (set hotkey or use a keyword (default: `!hc`))
2. by listing your books grouped by shelf (default keyword: `!hs`)
3. by listing your books grouped by reading status (default keyword: `!ht`)
4. by listing your books grouped by rating (default keyword: `!hr`)
5. by searching the Hardcover catalog (this search is similar to `⌘-k` on the Hardcover website. Set hotkey, or default keyword: `!hd`) 

In the library and database search you can use sort flags to sort your results:
- `--y`: by year (newest first)
- `--r`: by rating (highest first)
- `--t`: by title (alphabetical)

A couple of other things:
- In most visualizations, `⌘-⌥`(command-option) will move back to the previous visualization
- `::hardcover-refresh` will force database refresh 

That's it! Let me know if anything does not work, or if you'd like to add features. 


<h1 id="known-issues">Limitations & known issues ⚠️</h1>
None known for now, but if you see anything, let me know. 

<h1 id="changelog">Changelog 🧰</h1>
- 2025-02-17 First release (v0.1)

<h1 id="roadmap">Roadmap 🛣️</h1>
I don't think I will use any of these below, but if others are interested these are some of the possible next steps:

1. set reading progress
2. being able to search by shelf membership (e.g. `Lincoln` in `Biographies`)

<h1 id="acknowledgments">Acknowledgments 😀</h1>

- the Hardcover Discord community
- chatGPT and DeepSeek 
- Icones from www.flaticon.com
    - https://www.flaticon.com/free-icon/books_1258297?
    - https://www.flaticon.com/free-icon/star_465589
    - https://www.flaticon.com/free-icon/book_3330314
    - https://www.flaticon.com/free-icon/refresh_391192
    - https://www.flaticon.com/free-icon/open-book_9809619
    - https://www.flaticon.com/free-icon/books_8593492
    - https://www.flaticon.com/free-icon/book_4683540
    - https://www.flaticon.com/free-icon/star_471658
    - https://www.flaticon.com/free-icon/step-ladder_5597070
    - https://www.flaticon.com/free-icon/bookshelf_2570015

<h1 id="feedback">Feedback 🧐</h1>

Feedback welcome! If you notice a bug, or have ideas for new features, please feel free to get in touch either here, on the [Alfred](https://www.alfredforum.com) forum, or the Hardcover Discord. 
