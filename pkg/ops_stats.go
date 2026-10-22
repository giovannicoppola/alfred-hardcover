package main

// Library stats: a Markdown summary for Alfred's Text View and a fuller HTML
// report. `-stats` is run by stats.sh from the Text View object, which the 📊
// row on the empty library search (!hc) opens. It prints the Text View JSON:
// the Markdown as the response, and two variables for the actions behind it
// (same names as alfred-readwise and alfred-michelin, so the wiring reads the
// same):
//	statsReportPath      ↩ opens this HTML file
//	statsReportMarkdown  ⌘↩ copies the Markdown
//
// Read-only: it reads books.db as the last refresh left it.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type statBook struct {
	BookID      int
	UserBookID  int
	Title       string
	Authors     []string
	Status      int
	MyRating    float64
	Community   float64
	Raters      int
	ReleaseYear int
	Slug        string
	Shelves     []string
	Finished    []string // finish dates, yyyy-mm-dd
	Days        []int    // started → finished, where both are known
}

type libraryStats struct {
	Books     []*statBook
	Refreshed string
	Generated time.Time
}

func (b *statBook) author() string {
	if len(b.Authors) == 0 {
		return "Unknown"
	}
	return b.Authors[0]
}

// credited drops books with no credit at all, so "Unknown" never ranks as an author.
func credited(books []*statBook) []*statBook {
	var out []*statBook
	for _, b := range books {
		if len(b.Authors) > 0 {
			out = append(out, b)
		}
	}
	return out
}

func (b *statBook) lastFinished() string {
	last := ""
	for _, f := range b.Finished {
		if f > last {
			last = f
		}
	}
	return last
}

func gatherLibraryStats(db *sql.DB) (*libraryStats, error) {
	s := &libraryStats{Generated: time.Now()}
	byBook := map[int]*statBook{}
	byUserBook := map[int]*statBook{}

	rows, err := db.Query(`SELECT book_id, COALESCE(user_book_id, 0), COALESCE(title, ''), COALESCE(status_id, 0),
		COALESCE(user_rating, 0), COALESCE(rating, 0), COALESCE(ratings_count, 0), COALESCE(release_year, 0),
		COALESCE(slug, '') FROM books`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		b := &statBook{}
		if err := rows.Scan(&b.BookID, &b.UserBookID, &b.Title, &b.Status, &b.MyRating, &b.Community,
			&b.Raters, &b.ReleaseYear, &b.Slug); err != nil {
			rows.Close()
			return nil, err
		}
		s.Books = append(s.Books, b)
		byBook[b.BookID] = b
		if b.UserBookID != 0 {
			byUserBook[b.UserBookID] = b
		}
	}
	rows.Close()

	// Authors proper first. A book with none (an anthology, a score) falls back
	// to whoever else is credited (editor, composer), but never to the
	// translator or narrator, who would otherwise top the most-read list.
	rows, err = db.Query(`SELECT book_id, COALESCE(name, ''), COALESCE(contribution, '') FROM author ORDER BY ID`)
	if err != nil {
		return nil, err
	}
	others := map[int][]string{}
	for rows.Next() {
		var id int
		var name, role string
		if err := rows.Scan(&id, &name, &role); err != nil {
			rows.Close()
			return nil, err
		}
		name = strings.Join(strings.Fields(name), " ") // "Robert  Burton"
		b := byBook[id]
		if b == nil || name == "" {
			continue
		}
		switch strings.ToLower(role) {
		case "", "author":
			b.Authors = append(b.Authors, name)
		case "translator", "narrator", "illustrator", "introduction", "foreword", "afterword":
		default:
			others[id] = append(others[id], name)
		}
	}
	rows.Close()
	for id, names := range others {
		if b := byBook[id]; b != nil && len(b.Authors) == 0 {
			b.Authors = names
		}
	}

	rows, err = db.Query(`SELECT user_book_id, COALESCE(name, '') FROM shelf`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ub int
		var name string
		if err := rows.Scan(&ub, &name); err != nil {
			rows.Close()
			return nil, err
		}
		if b := byUserBook[ub]; b != nil && name != "" {
			b.Shelves = append(b.Shelves, name)
		}
	}
	rows.Close()

	rows, err = db.Query(`SELECT user_book_id, COALESCE(started_at, ''), COALESCE(finished_at, '') FROM journey`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ub int
		var started, finished string
		if err := rows.Scan(&ub, &started, &finished); err != nil {
			rows.Close()
			return nil, err
		}
		b := byUserBook[ub]
		if b == nil || len(finished) < 10 {
			continue
		}
		b.Finished = append(b.Finished, finished[:10])
		if len(started) >= 10 {
			st, e1 := time.Parse("2006-01-02", started[:10])
			fi, e2 := time.Parse("2006-01-02", finished[:10])
			if e1 == nil && e2 == nil && !fi.Before(st) {
				b.Days = append(b.Days, int(fi.Sub(st).Hours()/24)+1)
			}
		}
	}
	rows.Close()

	if raw, err := os.ReadFile(filepath.Join(dataFolder, "lastUpdatedLocal")); err == nil && len(raw) >= 10 {
		s.Refreshed = string(raw[:10])
	}
	return s, nil
}

// ------------------------------------------------------------------ derived numbers

var statusLabel = map[int]string{1: "Want to read", 2: "Reading", 3: "Read", 4: "Did not finish", 0: "No status (shelf only)"}
var statusOrder = []int{3, 2, 1, 4, 0}

type kv struct {
	Key string
	N   int
	Sum float64 // sum of my ratings over the rated ones
	R   int     // how many of them are rated
	Sub int     // a second count, e.g. read among them
}

func (k kv) avg() string {
	if k.R == 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", k.Sum/float64(k.R))
}

func tally(books []*statBook, keys func(*statBook) []string, sub func(*statBook) bool) []kv {
	m := map[string]*kv{}
	for _, b := range books {
		for _, k := range keys(b) {
			e, ok := m[k]
			if !ok {
				e = &kv{Key: k}
				m[k] = e
			}
			e.N++
			if b.MyRating > 0 {
				e.Sum += b.MyRating
				e.R++
			}
			if sub != nil && sub(b) {
				e.Sub++
			}
		}
	}
	out := make([]kv, 0, len(m))
	for _, e := range m {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return strings.ToLower(out[i].Key) < strings.ToLower(out[j].Key)
	})
	return out
}

func (s *libraryStats) withStatus(st int) []*statBook {
	var out []*statBook
	for _, b := range s.Books {
		if b.Status == st {
			out = append(out, b)
		}
	}
	return out
}

func (s *libraryStats) rated() []*statBook {
	var out []*statBook
	for _, b := range s.Books {
		if b.MyRating > 0 {
			out = append(out, b)
		}
	}
	return out
}

// finishes lists every dated finish (a reread counts each time), newest first.
type finish struct {
	Date string
	Book *statBook
}

func (s *libraryStats) finishes() []finish {
	var out []finish
	for _, b := range s.Books {
		for _, f := range b.Finished {
			out = append(out, finish{f, b})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return out
}

func (s *libraryStats) finishesByYear() ([]int, map[int]int) {
	m := map[int]int{}
	for _, f := range s.finishes() {
		var y int
		fmt.Sscanf(f.Date[:4], "%d", &y)
		m[y]++
	}
	var years []int
	for y := range m {
		years = append(years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	return years, m
}

const gridYears = 10 // the month grid covers the last ten years with finishes

func (s *libraryStats) finishesByMonth() map[[2]int]int {
	m := map[[2]int]int{}
	for _, f := range s.finishes() {
		t, err := time.Parse("2006-01-02", f.Date)
		if err == nil {
			m[[2]int{t.Year(), int(t.Month())}]++
		}
	}
	years, _ := s.finishesByYear()
	if len(years) > gridYears {
		cut := years[gridYears-1]
		for k := range m {
			if k[0] < cut {
				delete(m, k)
			}
		}
	}
	return m
}

func (s *libraryStats) medianDays() (int, int) {
	var d []int
	for _, b := range s.Books {
		d = append(d, b.Days...)
	}
	if len(d) == 0 {
		return 0, 0
	}
	sort.Ints(d)
	return d[len(d)/2], len(d)
}

func (s *libraryStats) rereads() []*statBook {
	var out []*statBook
	for _, b := range s.Books {
		if len(b.Finished) > 1 {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Finished) != len(out[j].Finished) {
			return len(out[i].Finished) > len(out[j].Finished)
		}
		return out[i].Title < out[j].Title
	})
	return out
}

func (s *libraryStats) avgRating() float64 {
	r := s.rated()
	if len(r) == 0 {
		return 0
	}
	t := 0.0
	for _, b := range r {
		t += b.MyRating
	}
	return t / float64(len(r))
}

// minRaters keeps the comparison with the community to books enough people
// have rated for its average to mean something.
const minRaters = 25

type crowdRow struct {
	Book *statBook
	Diff float64
}

func (s *libraryStats) againstCrowd() (above, below []crowdRow, meanDiff float64, n int) {
	var all []crowdRow
	for _, b := range s.rated() {
		if b.Community > 0 && b.Raters >= minRaters {
			all = append(all, crowdRow{b, b.MyRating - b.Community})
		}
	}
	if len(all) == 0 {
		return nil, nil, 0, 0
	}
	t := 0.0
	for _, c := range all {
		t += c.Diff
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Diff > all[j].Diff })
	for _, c := range all {
		if c.Diff > 0 {
			above = append(above, c)
		}
	}
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Diff < 0 {
			below = append(below, all[i])
		}
	}
	return above, below, t / float64(len(all)), len(all)
}

var eras = []struct {
	Label  string
	Lo, Hi int
}{
	{"before 1500", math.MinInt32, 1499},
	{"1500–1799", 1500, 1799},
	{"1800s", 1800, 1899},
	{"1900–1949", 1900, 1949},
	{"1950–1999", 1950, 1999},
	{"2000s", 2000, 2009},
	{"2010s", 2010, 2019},
	{"2020s", 2020, 2029},
}

func eraOf(year int) string {
	if year == 0 { // Hardcover's "unknown"
		return ""
	}
	for _, e := range eras {
		if year >= e.Lo && year <= e.Hi {
			return e.Label
		}
	}
	return ""
}

// ------------------------------------------------------------------ shading

// Shades for the month grid, lightest to heaviest. A code block is the only
// place Alfred renders a fixed-width grid, so the "colours" are characters.
var statCal = []string{"·", "░", "▒", "▓", "█"}
var statMonths = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// statThresholds gives quartile cut-offs for the four non-empty shades, as in
// alfred-readwise: fixed cut-offs would be all pale or all dark.
func statThresholds(m map[[2]int]int) [3]int {
	var nz []int
	for _, c := range m {
		if c > 0 {
			nz = append(nz, c)
		}
	}
	if len(nz) == 0 {
		return [3]int{1, 2, 3}
	}
	sort.Ints(nz)
	var out [3]int
	for i, p := range []float64{0.25, 0.5, 0.75} {
		idx := int(float64(len(nz)) * p)
		if idx > len(nz)-1 {
			idx = len(nz) - 1
		}
		v := nz[idx]
		floor := 1
		if i > 0 {
			floor = out[i-1] + 1
		}
		if v < floor {
			v = floor // strictly increasing, or two shades would mean the same thing
		}
		out[i] = v
	}
	return out
}

func statLevel(n int, th [3]int) int {
	if n <= 0 {
		return 0
	}
	for i, t := range th {
		if n <= t {
			return i + 1
		}
	}
	return 4
}

func statBands(th [3]int) []string {
	band := func(lo, hi int) string {
		if lo == hi {
			return fmt.Sprint(lo)
		}
		return fmt.Sprintf("%d–%d", lo, hi)
	}
	return []string{"none", band(1, th[0]), band(th[0]+1, th[1]), band(th[1]+1, th[2]), fmt.Sprintf(">%d", th[2])}
}

func statYearRange(m map[[2]int]int) (int, int) {
	lo, hi := 0, 0
	for k := range m {
		if lo == 0 || k[0] < lo {
			lo = k[0]
		}
		if k[0] > hi {
			hi = k[0]
		}
	}
	return lo, hi
}

// ------------------------------------------------------------------ Markdown

func statInt(n int) string {
	s := fmt.Sprint(n)
	if n < 1000 && n > -1000 {
		return s
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// mdTable lays out rows as fixed-width columns inside a code block: Alfred's
// Markdown has no tables. align holds '<' or '>' per column.
func mdTable(headers []string, rows [][]string, align string) []string {
	if len(rows) == 0 {
		return nil
	}
	w := make([]int, len(headers))
	for i, h := range headers {
		w[i] = utf8.RuneCountInString(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if n := utf8.RuneCountInString(c); n > w[i] {
				w[i] = n
			}
		}
	}
	line := func(cells []string) string {
		parts := make([]string, len(cells))
		for i, c := range cells {
			pad := strings.Repeat(" ", w[i]-utf8.RuneCountInString(c))
			if align[i] == '>' {
				parts[i] = pad + c
			} else {
				parts[i] = c + pad
			}
		}
		return strings.TrimRight(strings.Join(parts, "  "), " ")
	}
	dashes := make([]string, len(headers))
	for i := range headers {
		dashes[i] = strings.Repeat("-", w[i])
	}
	out := []string{"```", line(headers), line(dashes)}
	for _, r := range rows {
		out = append(out, line(r))
	}
	return append(out, "```", "")
}

func mdBar(v, max, width int) string {
	if max <= 0 || v <= 0 {
		return ""
	}
	n := (v*width + max/2) / max
	if n < 1 {
		n = 1
	}
	return strings.Repeat("▇", n)
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func ratingStr(r float64) string {
	if r <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", r)
}

func mdMonthGrid(m map[[2]int]int) []string {
	lo, hi := statYearRange(m)
	th := statThresholds(m)
	head := "      "
	for _, a := range statMonths {
		head += fmt.Sprintf("%-4s", a)
	}
	out := []string{"```", head + "  n"}
	for y := lo; y <= hi; y++ {
		line := fmt.Sprintf("%-6d", y)
		total := 0
		for mo := 1; mo <= 12; mo++ {
			n := m[[2]int{y, mo}]
			total += n
			line += " " + statCal[statLevel(n, th)] + "  "
		}
		out = append(out, line+fmt.Sprintf("%3d", total))
	}
	legend := make([]string, 5)
	for i, b := range statBands(th) {
		legend[i] = statCal[i] + " " + b
	}
	return append(out, "", "      "+strings.Join(legend, "  ")+"   (books finished per month)", "```", "")
}

func libraryMarkdown(s *libraryStats) string {
	read := s.withStatus(3)
	fin := s.finishes()
	md := []string{"# 📘 Your Hardcover library", ""}
	md = append(md, fmt.Sprintf("**%s** books · **%s** read · **%s** finishes on record · **%s** rated, "+
		"averaging **%.2f**.", statInt(len(s.Books)), statInt(len(read)), statInt(len(fin)),
		statInt(len(s.rated())), s.avgRating()))
	if s.Refreshed != "" {
		md = append(md, "", fmt.Sprintf("As of the last refresh, %s (`::hardcover-refresh` to update).", s.Refreshed))
	}
	md = append(md, "")

	md = append(md, "## Reading status", "")
	var rows [][]string
	for _, st := range statusOrder {
		n := len(s.withStatus(st))
		if n == 0 {
			continue
		}
		rows = append(rows, []string{statusLabel[st], statInt(n), mdBar(n, len(s.Books), 16)})
	}
	md = append(md, mdTable([]string{"status", "books", ""}, rows, "<><")...)
	if cur := s.withStatus(2); len(cur) > 0 {
		md = append(md, "Reading now:", "")
		rows = nil
		for _, b := range cur {
			rows = append(rows, []string{clip(b.Title, 40), clip(b.author(), 24)})
		}
		md = append(md, mdTable([]string{"book", "author"}, rows, "<<")...)
	}

	years, byYear := s.finishesByYear()
	if len(years) > 0 {
		md = append(md, "## Books finished by year", "")
		mx := 0
		for _, n := range byYear {
			if n > mx {
				mx = n
			}
		}
		rows = nil
		for _, y := range years {
			rows = append(rows, []string{fmt.Sprint(y), statInt(byYear[y]), mdBar(byYear[y], mx, 18)})
		}
		md = append(md, mdTable([]string{"year", "books", ""}, rows, "<><")...)
		if med, n := s.medianDays(); n > 0 {
			md = append(md, fmt.Sprintf("Median time from start to finish: **%d days** (%s books with both dates).",
				med, statInt(n)), "")
		}
		if rr := s.rereads(); len(rr) > 0 {
			names := []string{}
			for _, b := range rr {
				names = append(names, fmt.Sprintf("*%s* (%d×)", b.Title, len(b.Finished)))
			}
			md = append(md, "Reread: "+strings.Join(names, ", ")+".", "")
		}
		md = append(md, "## Month by month", "", fmt.Sprintf("The last %d years with finishes.", gridYears), "")
		md = append(md, mdMonthGrid(s.finishesByMonth())...)
	}

	md = append(md, "## Your ratings", "")
	rated := s.rated()
	counts := map[float64]int{}
	mx := 0
	for _, b := range rated {
		counts[b.MyRating]++
		if counts[b.MyRating] > mx {
			mx = counts[b.MyRating]
		}
	}
	rows = nil
	for r := 5.0; r >= 0.5; r -= 0.5 {
		if counts[r] == 0 {
			continue
		}
		rows = append(rows, []string{fmt.Sprintf("%.1f", r), statInt(counts[r]), mdBar(counts[r], mx, 18)})
	}
	md = append(md, mdTable([]string{"rating", "books", ""}, rows, "<><")...)
	if above, below, diff, n := s.againstCrowd(); n > 0 {
		md = append(md, fmt.Sprintf("Against the Hardcover community (%s rated books with at least %d ratings): "+
			"you rate **%+.2f** on average.", statInt(n), minRaters, diff), "")
		crowd := func(title string, list []crowdRow) {
			if len(list) == 0 {
				return
			}
			rows = nil
			for i, c := range list {
				if i == 6 {
					break
				}
				rows = append(rows, []string{clip(c.Book.Title, 34), ratingStr(c.Book.MyRating),
					fmt.Sprintf("%.2f", c.Book.Community), fmt.Sprintf("%+.1f", c.Diff)})
			}
			md = append(md, title, "")
			md = append(md, mdTable([]string{"book", "you", "crowd", "diff"}, rows, "<>>>")...)
		}
		crowd("You liked more than most:", above)
		crowd("You liked less than most:", below)
	}

	md = append(md, "## Most-read authors", "")
	rows = nil
	for i, a := range tally(credited(read), func(b *statBook) []string { return []string{b.author()} }, nil) {
		if i == 12 {
			break
		}
		rows = append(rows, []string{clip(a.Key, 28), statInt(a.N), a.avg()})
	}
	md = append(md, mdTable([]string{"author", "read", "avg rating"}, rows, "<>>")...)

	md = append(md, "## Shelves", "")
	rows = nil
	for _, sh := range tally(s.Books, func(b *statBook) []string { return b.Shelves }, func(b *statBook) bool { return b.Status == 3 }) {
		rows = append(rows, []string{clip(sh.Key, 24), statInt(sh.N), statInt(sh.Sub), sh.avg()})
	}
	if len(rows) == 0 {
		md = append(md, "No shelves yet.", "")
	} else {
		md = append(md, mdTable([]string{"shelf", "books", "read", "avg rating"}, rows, "<>>>")...)
	}

	md = append(md, "## When they were written", "", "Books read, by first publication.", "")
	eraN := map[string]int{}
	for _, b := range read {
		eraN[eraOf(b.ReleaseYear)]++
	}
	rows = nil
	emx := 0
	for _, e := range eras {
		if eraN[e.Label] > emx {
			emx = eraN[e.Label]
		}
	}
	for _, e := range eras {
		if n := eraN[e.Label]; n > 0 {
			rows = append(rows, []string{e.Label, statInt(n), mdBar(n, emx, 16)})
		}
	}
	md = append(md, mdTable([]string{"written", "read", ""}, rows, "<><")...)

	if len(fin) > 0 {
		md = append(md, "## Recently finished", "")
		rows = nil
		for i, f := range fin {
			if i == 10 {
				break
			}
			rows = append(rows, []string{clip(f.Book.Title, 34), clip(f.Book.author(), 20), f.Date, ratingStr(f.Book.MyRating)})
		}
		md = append(md, mdTable([]string{"book", "author", "finished", "rating"}, rows, "<<<>")...)
	}
	md = append(md, "*↩ opens the full HTML report · ⌘↩ copies this summary*")
	return strings.Join(md, "\n")
}

// ------------------------------------------------------------------ HTML

const statsCSS = `
:root { color-scheme: light dark;
  --bg:#f8f8fb; --panel:#ffffff; --ink:#1c1c24; --muted:#666879; --line:#e3e3ec;
  --accent:#5b4bd6; --l0:#ececf3; --l1:#d4cff6; --l2:#aca2ee; --l3:#8474e2; --l4:#5b4bd6; }
@media (prefers-color-scheme: dark) { :root {
  --bg:#111118; --panel:#1a1a23; --ink:#e7e7ef; --muted:#9c9db0; --line:#2c2c38;
  --accent:#9d90ff; --l0:#22222c; --l1:#302a5c; --l2:#4b3f96; --l3:#7466d6; --l4:#a597ff; } }
* { box-sizing:border-box; }
body { margin:0; background:var(--bg); color:var(--ink);
  font:15px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif; }
.wrap { max-width:980px; margin:0 auto; padding:40px 24px 96px; }
h1 { font-size:30px; margin:0 0 6px; letter-spacing:-0.02em; }
h2 { font-size:20px; margin:44px 0 14px; letter-spacing:-0.01em; }
h3 { font-size:13px; margin:24px 0 10px; color:var(--muted); font-weight:600;
  text-transform:uppercase; letter-spacing:0.06em; }
p.lede { color:var(--muted); margin:0 0 20px; }
a { color:var(--accent); text-decoration:none; } a:hover { text-decoration:underline; }
.tiles { display:grid; grid-template-columns:repeat(auto-fit, minmax(130px, 1fr)); gap:12px; }
.tile { background:var(--panel); border:1px solid var(--line); border-radius:10px; padding:14px 16px; }
.tile .n { font-size:24px; font-weight:650; letter-spacing:-0.02em; font-variant-numeric:tabular-nums; }
.tile .k { font-size:12px; color:var(--muted); text-transform:uppercase; letter-spacing:0.06em; }
.tablewrap { overflow-x:auto; background:var(--panel); border:1px solid var(--line); border-radius:10px; }
table { width:100%; border-collapse:collapse; font-size:14px; }
th, td { text-align:left; padding:7px 10px; border-bottom:1px solid var(--line); }
tr:last-child td { border-bottom:0; }
th { font-size:11px; text-transform:uppercase; letter-spacing:0.07em; color:var(--muted); font-weight:600; }
td.n, th.n { text-align:right; font-variant-numeric:tabular-nums; white-space:nowrap; }
td.bar, th.bar { width:22%; min-width:80px; }
td.m { color:var(--muted); }
.meter { display:block; height:7px; border-radius:4px; background:var(--accent); min-width:2px; }
.tag { display:inline-block; font-size:12px; color:var(--muted); border:1px solid var(--line);
  border-radius:9px; padding:0 7px; margin:1px 3px 1px 0; }
.grid td { padding:3px; border:0; text-align:center; font-size:11px; color:var(--muted); }
.grid td.y { text-align:left; padding-right:10px; font-variant-numeric:tabular-nums; }
.grid td.t { text-align:right; padding-left:10px; font-variant-numeric:tabular-nums; }
.cell { width:100%; min-width:18px; height:18px; border-radius:4px; }
.legend { display:flex; align-items:center; gap:4px; color:var(--muted); font-size:12px; margin:8px 0 0; }
.legend i { display:inline-block; width:12px; height:12px; border-radius:3px; }
.two { display:grid; grid-template-columns:1fr 1fr; gap:16px; }
ul.five { columns:2; column-gap:28px; padding-left:18px; margin:0; }
ul.five li { break-inside:avoid; margin:0 0 4px; }
ul.five .m { color:var(--muted); }
input.filter { width:100%; padding:9px 12px; margin:0 0 12px; font:inherit; color:var(--ink);
  background:var(--panel); border:1px solid var(--line); border-radius:8px; }
@media (max-width:720px) { .two { grid-template-columns:1fr; } ul.five { columns:1; } .wrap { padding:28px 16px 64px; } }
.collapsible:not(.open) .more { display:none; }
.more-toggle { margin:10px 0 0; padding:6px 12px; font:inherit; font-size:13px; color:var(--accent);
  background:var(--panel); border:1px solid var(--line); border-radius:8px; cursor:pointer; }
.more-toggle:hover { border-color:var(--accent); }
.modes { display:flex; align-items:center; gap:6px; margin:-4px 0 12px; font-size:13px; color:var(--muted); }
.modes span { margin-right:4px; }
.modes button, .pager button { font:inherit; font-size:13px; padding:5px 11px; color:var(--ink);
  background:var(--panel); border:1px solid var(--line); border-radius:8px; cursor:pointer; }
.modes button:hover, .pager button:hover:not(:disabled) { border-color:var(--accent); }
.modes button.on, .pager button.on { background:var(--accent); border-color:var(--accent); color:var(--bg); }
.pager { display:flex; flex-wrap:wrap; align-items:center; gap:6px; margin:12px 0 0; }
.pager .info { color:var(--muted); font-size:13px; margin-right:auto; font-variant-numeric:tabular-nums; }
.pager button:disabled { opacity:.4; cursor:default; }
.pager .gap { color:var(--muted); padding:0 2px; }
footer { margin-top:60px; color:var(--muted); font-size:12px; border-top:1px solid var(--line); padding-top:16px; }
`

const statsJS = `
<script>
// The full list (#all): filtered by the box above it, then shown 25 rows at a
// time, either as pages or as an infinite scroll. The choice is remembered.
(function () {
  var table = document.getElementById('all'), box = document.getElementById('filter');
  if (!table || !box) return;
  var SIZE = 25, page = 0, limit = SIZE, mode = 'pages';
  var rows = Array.prototype.slice.call(table.querySelectorAll('tr[data-s]')), matches = rows;
  try { mode = localStorage.getItem('listMode') === 'scroll' ? 'scroll' : 'pages'; } catch (e) {}

  var modes = document.createElement('div');
  modes.className = 'modes';
  modes.innerHTML = '<span>Show as</span><button type="button" data-m="pages">Pages</button>' +
    '<button type="button" data-m="scroll">Infinite scroll</button>';
  box.parentNode.insertBefore(modes, box.nextSibling);
  var wrap = table.parentNode;
  var pager = document.createElement('nav');
  pager.className = 'pager';
  wrap.parentNode.insertBefore(pager, wrap.nextSibling);

  function button(label, onClick, disabled, current) {
    var b = document.createElement('button');
    b.type = 'button';
    b.textContent = label;
    b.disabled = !!disabled;
    if (current) b.className = 'on';
    b.addEventListener('click', onClick);
    pager.appendChild(b);
  }
  function goTo(p) {
    return function () {
      page = p;
      render();
      var top = wrap.getBoundingClientRect().top;
      if (top < 0) window.scrollBy(0, top - 16);
    };
  }
  // first, last, and the pages either side of the current one; gaps as "…"
  function pageNumbers(n) {
    var out = [], last = -1;
    for (var i = 0; i < n; i++) {
      if (i === 0 || i === n - 1 || Math.abs(i - page) <= 1) {
        if (last >= 0 && i - last > 1) out.push(-1);
        out.push(i);
        last = i;
      }
    }
    return out;
  }
  function render() {
    var start = mode === 'pages' ? page * SIZE : 0;
    var end = Math.min(mode === 'pages' ? start + SIZE : limit, matches.length);
    rows.forEach(function (r) { r.style.display = 'none'; });
    matches.slice(start, end).forEach(function (r) { r.style.display = ''; });
    Array.prototype.forEach.call(modes.querySelectorAll('button'), function (b) {
      b.classList.toggle('on', b.dataset.m === mode);
    });
    pager.innerHTML = '';
    var info = document.createElement('span');
    info.className = 'info';
    info.textContent = matches.length
      ? (start + 1).toLocaleString() + '–' + end.toLocaleString() + ' of ' + matches.length.toLocaleString()
      : 'Nothing matches';
    pager.appendChild(info);
    if (mode === 'pages') {
      var pages = Math.ceil(matches.length / SIZE);
      if (pages > 1) {
        button('‹ Prev', goTo(page - 1), page === 0);
        pageNumbers(pages).forEach(function (p) {
          if (p < 0) {
            var gap = document.createElement('span');
            gap.className = 'gap';
            gap.textContent = '…';
            pager.appendChild(gap);
          } else {
            button(String(p + 1), goTo(p), false, p === page);
          }
        });
        button('Next ›', goTo(page + 1), page === pages - 1);
      }
    } else if (end < matches.length) {
      button('Load more', function () { limit += SIZE; render(); });
    }
  }
  // infinite scroll: top up while the bottom of the list is near the viewport
  function topUp() {
    if (mode !== 'scroll' || limit >= matches.length) return;
    if (pager.getBoundingClientRect().top < window.innerHeight + 400) {
      limit += SIZE;
      render();
      window.requestAnimationFrame(topUp);
    }
  }
  window.addEventListener('scroll', topUp, { passive: true });

  box.addEventListener('input', function () {
    var words = box.value.toLowerCase().split(' ').filter(Boolean);
    matches = rows.filter(function (r) {
      return words.every(function (w) { return r.dataset.s.indexOf(w) !== -1; });
    });
    page = 0;
    limit = SIZE;
    render();
  });
  modes.addEventListener('click', function (ev) {
    var m = ev.target.dataset ? ev.target.dataset.m : null;
    if (!m || m === mode) return;
    mode = m;
    page = 0;
    limit = SIZE;
    try { localStorage.setItem('listMode', mode); } catch (e) {}
    render();
    topUp();
  });
  render();
})();
// "Show N more" on the shorter lists above
Array.prototype.forEach.call(document.querySelectorAll('.more-toggle'), function (btn) {
  var n = btn.dataset.n;
  btn.addEventListener('click', function () {
    var open = btn.parentNode.classList.toggle('open');
    btn.textContent = open ? 'Show fewer' : 'Show ' + Number(n).toLocaleString() + ' more';
  });
});
</script>
`

var hesc = html.EscapeString

func hMeter(v, max int) string {
	w := 0.0
	if max > 0 && v > 0 {
		w = math.Max(1.5, 100*float64(v)/float64(max))
	}
	return fmt.Sprintf(`<span class="meter" style="width:%.1f%%"></span>`, w)
}

func bookLink(b *statBook) string {
	if b.Slug == "" {
		return hesc(b.Title)
	}
	return fmt.Sprintf(`<a href="%s">%s</a>`, hesc(baseURL+b.Slug), hesc(b.Title))
}

// hTable: cells are already-escaped HTML; classes gives each column's class.
// Long lists show their first collapseShow rows and fold the rest behind a
// "Show N more" button. Only past collapseOver, so a button never hides a row
// or two; the filterable whole-library table is never folded.
const (
	collapseShow = 7
	collapseOver = 10
)

func folds(n int) bool { return n > collapseOver }

func moreButton(n int) string {
	return fmt.Sprintf(`<button type="button" class="more-toggle" data-n="%d">Show %s more</button>`, n, statInt(n))
}

func hTable(headers []string, rows [][]string, classes []string, id string, rowAttrs []string) string {
	var b strings.Builder
	fold := rowAttrs == nil && folds(len(rows))
	if fold {
		b.WriteString(`<div class="collapsible">`)
	}
	b.WriteString(`<div class="tablewrap"><table`)
	if id != "" {
		fmt.Fprintf(&b, ` id="%s"`, id)
	}
	b.WriteString("><tr>")
	for i, h := range headers {
		fmt.Fprintf(&b, `<th class="%s">%s</th>`, classes[i], hesc(h))
	}
	b.WriteString("</tr>\n")
	for k, r := range rows {
		b.WriteString("<tr")
		if rowAttrs != nil {
			b.WriteString(rowAttrs[k])
		}
		if fold && k >= collapseShow {
			b.WriteString(` class="more"`)
		}
		b.WriteString(">")
		for i, c := range r {
			fmt.Fprintf(&b, `<td class="%s">%s</td>`, classes[i], c)
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</table></div>\n")
	if fold {
		b.WriteString(moreButton(len(rows)-collapseShow) + "</div>\n")
	}
	return b.String()
}

func hTile(n, k string) string {
	return fmt.Sprintf(`<div class="tile"><div class="n">%s</div><div class="k">%s</div></div>`, hesc(n), hesc(k))
}

func hMonthGrid(m map[[2]int]int) string {
	lo, hi := statYearRange(m)
	th := statThresholds(m)
	var b strings.Builder
	b.WriteString(`<div class="tablewrap" style="padding:10px 12px"><table class="grid"><tr><td></td>`)
	for _, a := range statMonths {
		fmt.Fprintf(&b, "<td>%s</td>", a)
	}
	b.WriteString("<td></td></tr>\n")
	for y := hi; y >= lo; y-- {
		fmt.Fprintf(&b, `<tr><td class="y">%d</td>`, y)
		total := 0
		for mo := 1; mo <= 12; mo++ {
			n := m[[2]int{y, mo}]
			total += n
			plural := "s"
			if n == 1 {
				plural = ""
			}
			fmt.Fprintf(&b, `<td><div class="cell" style="background:var(--l%d)" title="%d book%s finished in %s %d"></div></td>`,
				statLevel(n, th), n, plural, statMonths[mo-1], y)
		}
		t := ""
		if total > 0 {
			t = fmt.Sprint(total)
		}
		fmt.Fprintf(&b, `<td class="t">%s</td></tr>`+"\n", t)
	}
	b.WriteString(`</table><div class="legend">less`)
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&b, `<i style="background:var(--l%d)"></i>`, i)
	}
	fmt.Fprintf(&b, `more <span style="margin-left:10px">(%s books per month)</span></div></div>`,
		hesc(strings.Join(statBands(th)[1:], ", ")))
	return b.String()
}

func libraryHTML(s *libraryStats) string {
	read := s.withStatus(3)
	fin := s.finishes()
	years, byYear := s.finishesByYear()
	var h strings.Builder
	h.WriteString("<!doctype html><html lang='en'><head><meta charset='utf-8'>" +
		"<meta name='viewport' content='width=device-width, initial-scale=1'>" +
		"<title>Hardcover stats</title><style>" + statsCSS + "</style></head><body><div class='wrap'>")
	h.WriteString("<h1>📘 Your Hardcover library</h1>")
	lede := "Generated " + s.Generated.Format("2 Jan 2006")
	if s.Refreshed != "" {
		lede += " from the library as of " + s.Refreshed
	}
	fmt.Fprintf(&h, "<p class='lede'>%s.</p>", hesc(lede))
	thisYear := byYear[s.Generated.Year()]
	h.WriteString(`<div class="tiles">` +
		hTile(statInt(len(s.Books)), "books") +
		hTile(statInt(len(read)), "read") +
		hTile(statInt(thisYear), fmt.Sprintf("finished in %d", s.Generated.Year())) +
		hTile(statInt(len(s.withStatus(1))), "want to read") +
		hTile(statInt(len(s.rated())), "rated") +
		hTile(fmt.Sprintf("%.2f", s.avgRating()), "average rating") + "</div>")

	h.WriteString(`<h2>Reading status</h2><div class="two"><div>`)
	var rows [][]string
	for _, st := range statusOrder {
		if n := len(s.withStatus(st)); n > 0 {
			rows = append(rows, []string{hesc(statusLabel[st]), statInt(n), hMeter(n, len(s.Books))})
		}
	}
	h.WriteString(hTable([]string{"Status", "Books", ""}, rows, []string{"", "n", "bar"}, "", nil))
	h.WriteString("</div><div>")
	rows = nil
	for _, b := range s.withStatus(2) {
		rows = append(rows, []string{bookLink(b), hesc(b.author())})
	}
	if len(rows) > 0 {
		h.WriteString(hTable([]string{"Reading now", "Author"}, rows, []string{"", "m"}, "", nil))
	}
	h.WriteString("</div></div>")

	if len(years) > 0 {
		h.WriteString("<h2>Books finished</h2>")
		var notes []string
		if med, n := s.medianDays(); n > 0 {
			notes = append(notes, fmt.Sprintf("median %d days from start to finish (%s books with both dates)", med, statInt(n)))
		}
		if rr := s.rereads(); len(rr) > 0 {
			var names []string
			for _, b := range rr {
				names = append(names, fmt.Sprintf("%s (%d×)", b.Title, len(b.Finished)))
			}
			notes = append(notes, "reread: "+strings.Join(names, ", "))
		}
		fmt.Fprintf(&h, "<p class='lede'>%s finishes on record; %s.</p>", statInt(len(fin)), hesc(strings.Join(notes, "; ")))
		h.WriteString(hMonthGrid(s.finishesByMonth()))
		h.WriteString("<h3>By year</h3>")
		mx := 0
		for _, n := range byYear {
			if n > mx {
				mx = n
			}
		}
		rows = nil
		for _, y := range years {
			rows = append(rows, []string{fmt.Sprint(y), statInt(byYear[y]), hMeter(byYear[y], mx)})
		}
		h.WriteString(hTable([]string{"Year", "Books", ""}, rows, []string{"", "n", "bar"}, "", nil))
	}

	h.WriteString("<h2>Your ratings</h2>")
	counts := map[float64]int{}
	cmx := 0
	for _, b := range s.rated() {
		counts[b.MyRating]++
		if counts[b.MyRating] > cmx {
			cmx = counts[b.MyRating]
		}
	}
	rows = nil
	for r := 5.0; r >= 0.5; r -= 0.5 {
		if counts[r] > 0 {
			rows = append(rows, []string{fmt.Sprintf("%.1f", r), statInt(counts[r]), hMeter(counts[r], cmx)})
		}
	}
	h.WriteString(hTable([]string{"Rating", "Books", ""}, rows, []string{"", "n", "bar"}, "", nil))
	if above, below, diff, n := s.againstCrowd(); n > 0 {
		fmt.Fprintf(&h, "<p class='lede' style='margin-top:10px'>Against the Hardcover community (%s rated books "+
			"with at least %d ratings) you rate %+.2f on average.</p>", statInt(n), minRaters, diff)
		h.WriteString(`<div class="two"><div>`)
		crowd := func(title string, list []crowdRow) {
			rows = nil
			for i, c := range list {
				if i == 10 {
					break
				}
				rows = append(rows, []string{bookLink(c.Book), ratingStr(c.Book.MyRating),
					fmt.Sprintf("%.2f", c.Book.Community), fmt.Sprintf("%+.1f", c.Diff)})
			}
			h.WriteString("<h3>" + hesc(title) + "</h3>")
			h.WriteString(hTable([]string{"Book", "You", "Crowd", "Diff"}, rows, []string{"", "n", "n", "n"}, "", nil))
		}
		crowd("You liked more than most", above)
		h.WriteString("</div><div>")
		crowd("You liked less than most", below)
		h.WriteString("</div></div>")
	}

	h.WriteString(`<h2>Authors and shelves</h2><div class="two"><div>`)
	authors := tally(credited(read), func(b *statBook) []string { return []string{b.author()} }, nil)
	rows = nil
	for i, a := range authors {
		if i == 25 {
			break
		}
		rows = append(rows, []string{hesc(a.Key), statInt(a.N), a.avg()})
	}
	h.WriteString(hTable([]string{"Most-read author", "Read", "Avg"}, rows, []string{"", "n", "n"}, "", nil))
	h.WriteString("</div><div>")
	rows = nil
	for _, sh := range tally(s.Books, func(b *statBook) []string { return b.Shelves }, func(b *statBook) bool { return b.Status == 3 }) {
		rows = append(rows, []string{hesc(sh.Key), statInt(sh.N), statInt(sh.Sub), sh.avg()})
	}
	h.WriteString(hTable([]string{"Shelf", "Books", "Read", "Avg"}, rows, []string{"", "n", "n", "n"}, "", nil))
	h.WriteString("</div></div>")

	h.WriteString("<h2>When they were written</h2><p class='lede'>Books read, by first publication.</p>")
	eraN := map[string]int{}
	for _, b := range read {
		eraN[eraOf(b.ReleaseYear)]++
	}
	emx := 0
	for _, e := range eras {
		if eraN[e.Label] > emx {
			emx = eraN[e.Label]
		}
	}
	rows = nil
	for _, e := range eras {
		if n := eraN[e.Label]; n > 0 {
			rows = append(rows, []string{e.Label, statInt(n), hMeter(n, emx)})
		}
	}
	h.WriteString(hTable([]string{"Written", "Read", ""}, rows, []string{"", "n", "bar"}, "", nil))

	var five []*statBook
	for _, b := range s.Books {
		if b.MyRating == 5 {
			five = append(five, b)
		}
	}
	if len(five) > 0 {
		sort.Slice(five, func(i, j int) bool { return five[i].lastFinished() > five[j].lastFinished() })
		fmt.Fprintf(&h, "<h2>Your five-star books</h2><p class='lede'>%s books, most recently finished first.</p>",
			statInt(len(five)))
		fold := folds(len(five))
		if fold {
			h.WriteString(`<div class="collapsible">`)
		}
		h.WriteString("<ul class='five'>")
		for i, b := range five {
			more := ""
			if fold && i >= collapseShow {
				more = ` class="more"`
			}
			fmt.Fprintf(&h, `<li%s>%s <span class="m">· %s</span></li>`, more, bookLink(b), hesc(b.author()))
		}
		h.WriteString("</ul>")
		if fold {
			h.WriteString(moreButton(len(five)-collapseShow) + "</div>")
		}
	}

	all := append([]*statBook(nil), s.Books...)
	sort.SliceStable(all, func(i, j int) bool {
		fi, fj := all[i].lastFinished(), all[j].lastFinished()
		if fi != fj {
			return fi > fj
		}
		return strings.ToLower(all[i].Title) < strings.ToLower(all[j].Title)
	})
	fmt.Fprintf(&h, "<h2>The whole library</h2><p class='lede'>%s books, most recently finished first.</p>", statInt(len(all)))
	h.WriteString(`<input id="filter" class="filter" type="search" placeholder="Filter by title, author, status, shelf or year…" autofocus>`)
	rows = nil
	var attrs []string
	for _, b := range all {
		cell := bookLink(b)
		for _, sh := range b.Shelves {
			cell += ` <span class="tag">` + hesc(sh) + `</span>`
		}
		rows = append(rows, []string{cell, hesc(b.author()), hesc(statusLabel[b.Status]), ratingStr(b.MyRating), hesc(b.lastFinished())})
		attrs = append(attrs, fmt.Sprintf(` data-s="%s"`, hesc(strings.ToLower(strings.Join(
			append([]string{b.Title, strings.Join(b.Authors, " "), statusLabel[b.Status], b.lastFinished()}, b.Shelves...), " ")))))
	}
	h.WriteString(hTable([]string{"Book", "Author", "Status", "Rating", "Finished"}, rows,
		[]string{"", "m", "m", "n", "m"}, "all", attrs))

	fmt.Fprintf(&h, "<footer>alfred-hardcover · generated %s</footer>", s.Generated.Format("2006-01-02 15:04"))
	h.WriteString("</div>" + statsJS + "</body></html>")
	return h.String()
}

// showStats prints the Text View JSON and writes the HTML report next to books.db.
func showStats() {
	out := map[string]interface{}{"behaviour": map[string]string{"response": "replace", "scroll": "start"}}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)

	db, err := sql.Open("sqlite3", databasePath)
	if err == nil {
		defer db.Close()
	}
	var s *libraryStats
	if err == nil {
		s, err = gatherLibraryStats(db)
	}
	if err != nil {
		out["response"] = fmt.Sprintf("# Stats unavailable\n\n`%v`", err)
		enc.Encode(out)
		return
	}
	if len(s.Books) == 0 {
		out["response"] = "# 📘 Your Hardcover library\n\nThe library is empty: run `::hardcover-refresh` first."
		enc.Encode(out)
		return
	}
	md := libraryMarkdown(s)
	reportPath := filepath.Join(dataFolder, "hardcover-report.html")
	if err := os.WriteFile(reportPath, []byte(libraryHTML(s)), 0644); err != nil {
		LogF("failed to write %s: %v", reportPath, err)
	}
	out["response"] = md
	out["footer"] = "↩ open the HTML report · ⌘↩ copy this summary"
	out["variables"] = map[string]string{"statsReportPath": reportPath, "statsReportMarkdown": md}
	enc.Encode(out)
}
