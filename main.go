package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	_ "embed"
)

type Profile struct {
	Name     string   `json:"name"`
	Headline string   `json:"headline"`
	Location string   `json:"location,omitempty"`
	URL      string   `json:"url"`
	Emails   []string `json:"emails,omitempty"`
	Phones   []string `json:"phones,omitempty"`
	Snippet  string   `json:"snippet"`
}

type serpResp struct {
	Error      string `json:"error"`
	SearchInfo struct {
		TotalResults int64 `json:"total_results"`
	} `json:"search_information"`
	Pagination struct {
		OtherPages map[string]any `json:"other_pages"`
	} `json:"serpapi_pagination"`
	Organic []struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
	} `json:"organic_results"`
}

type SearchMeta struct {
	StartPage  int `json:"start_page"`
	LastPage   int `json:"last_page"`   // last page actually scraped (StartPage-1 if none)
	TotalPages int `json:"total_pages"` // estimate; 0 = unknown
}

const (
	resultsPerPage = 10
	maxPagesCap    = 30 // Google rarely serves deeper than this; adjust if you see otherwise
)

var (
	emailRe = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	phoneRe = regexp.MustCompile(`\+?\d[\d\s().\-]{7,}\d`)
	locRe   = regexp.MustCompile(`(?i)location:\s*([^·|]+)`)
	client  = &http.Client{Timeout: 30 * time.Second}
)

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func digits(s string) int {
	n := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n++
		}
	}
	return n
}

func parse(title, link, snip string) Profile {
	t := strings.TrimSpace(title)
	t = strings.TrimSpace(strings.TrimSuffix(t, "| LinkedIn"))
	t = strings.ReplaceAll(t, " – ", " - ")
	parts := strings.Split(t, " - ")

	p := Profile{Name: strings.TrimSpace(parts[0]), URL: link, Snippet: snip}
	if len(parts) > 1 {
		p.Headline = strings.TrimSpace(strings.Join(parts[1:], " - "))
	}
	blob := title + " " + snip
	p.Emails = uniq(emailRe.FindAllString(blob, -1))
	for _, ph := range phoneRe.FindAllString(blob, -1) {
		if d := digits(ph); d >= 9 && d <= 15 {
			p.Phones = append(p.Phones, ph)
		}
	}
	p.Phones = uniq(p.Phones)
	if m := locRe.FindStringSubmatch(snip); m != nil {
		p.Location = strings.TrimSpace(m[1])
	}
	return p
}

// ---------- Pagination helpers ----------

// estimate total pages from total_results and the pagination links SerpAPI returns
func estimateTotalPages(sr *serpResp) int {
	n := int((sr.SearchInfo.TotalResults + resultsPerPage - 1) / resultsPerPage)
	for k := range sr.Pagination.OtherPages {
		if p, err := strconv.Atoi(k); err == nil && p > n {
			n = p
		}
	}
	if n > maxPagesCap {
		n = maxPagesCap
	}
	return n
}

func totalLabel(total int) string {
	if total <= 0 {
		return "?"
	}
	return strconv.Itoa(total)
}

func pageSummary(m SearchMeta) string {
	scraped := "none"
	if m.LastPage >= m.StartPage {
		scraped = fmt.Sprintf("%d-%d", m.StartPage, m.LastPage)
	}
	return fmt.Sprintf("Pages available: %s (estimated) | Scraped: %s", totalLabel(m.TotalPages), scraped)
}

// ---------- Search ----------

// search scrapes `pages` pages beginning at startPage (1-based).
// onPage (optional) is called before each page's results are processed.
func search(key, q string, startPage, pages int, onPage func(cur, total int)) ([]Profile, SearchMeta, error) {
	if startPage < 1 {
		startPage = 1
	}
	meta := SearchMeta{StartPage: startPage, LastPage: startPage - 1}
	if key == "" {
		return nil, meta, fmt.Errorf("missing SerpAPI key")
	}
	seen := map[string]bool{}
	var out []Profile
	for i := 0; i < pages; i++ {
		pg := startPage + i
		v := url.Values{
			"engine":  {"google"},
			"q":       {q},
			"start":   {strconv.Itoa((pg - 1) * resultsPerPage)},
			"api_key": {key},
		}
		resp, err := client.Get("https://serpapi.com/search?" + v.Encode())
		if err != nil {
			return out, meta, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var sr serpResp
		if err := json.Unmarshal(body, &sr); err != nil {
			return out, meta, fmt.Errorf("bad response (HTTP %d): %w", resp.StatusCode, err)
		}
		if sr.Error != "" {
			// SerpAPI says this when a page has no more results
			if len(out) > 0 && strings.Contains(strings.ToLower(sr.Error), "hasn't returned any results") {
				break
			}
			return out, meta, fmt.Errorf("serpapi: %s", sr.Error)
		}
		if len(sr.Organic) == 0 {
			break
		}

		if t := estimateTotalPages(&sr); t > meta.TotalPages {
			meta.TotalPages = t
		}
		if onPage != nil {
			total := meta.TotalPages
			if total > 0 && total < pg {
				total = pg // estimate was low; never show "page 7 of 6"
			}
			onPage(pg, total)
		}

		for _, r := range sr.Organic {
			if !strings.Contains(r.Link, "linkedin.com/in/") || seen[r.Link] {
				continue
			}
			seen[r.Link] = true
			out = append(out, parse(r.Title, r.Link, r.Snippet))
		}
		meta.LastPage = pg
		time.Sleep(500 * time.Millisecond)
	}
	return out, meta, nil
}

// ---------- CLI ----------

func runCLI(args []string) {
	fs := flag.NewFlagSet("cli", flag.ExitOnError)
	q := fs.String("q", `site:linkedin.com/in/ "Marriott International"`, "dork")
	pages := fs.Int("pages", 3, "number of pages to scrape (10 per page)")
	start := fs.Int("start", 1, "page number to start scraping from")
	key := fs.String("key", os.Getenv("SERPAPI_KEY"), "SerpAPI key (or SERPAPI_KEY env)")
	format := fs.String("f", "table", "table | json | csv")
	out := fs.String("o", "", "write results to this JSON file (suppresses stdout unless -f is set)")
	fs.Parse(args)

	res, meta, err := search(*key, *q, *start, *pages, func(cur, total int) {
		fmt.Fprintf(os.Stderr, "Scraping page %d of %s\n", cur, totalLabel(total))
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if len(res) == 0 {
			os.Exit(1)
		}
	}
	summary := pageSummary(meta)
	if *out != "" {
		if err := writeJSONFile(*out, res); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "wrote %d profiles to %s\n", len(res), *out)
		fFlagSet := false
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "f" {
				fFlagSet = true
			}
		})
		if !fFlagSet {
			fmt.Fprintln(os.Stderr, summary)
			return
		}
	}
	switch *format {
	case "json":
		fmt.Fprintln(os.Stderr, summary) // keep stdout machine-readable
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(res)
	case "csv":
		fmt.Fprintln(os.Stderr, summary)
		writeCSV(os.Stdout, res)
	default:
		fmt.Println(summary) // top of the table
		fmt.Println()
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tHEADLINE\tLOCATION\tEMAIL\tPHONE\tURL")
		for _, p := range res {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, trunc(p.Headline, 50), p.Location,
				strings.Join(p.Emails, ","), strings.Join(p.Phones, ","), p.URL)
		}
		tw.Flush()
	}
	fmt.Fprintf(os.Stderr, "%d profiles\n", len(res))
}

func writeJSONFile(path string, res []Profile) error {
	if res == nil {
		res = []Profile{}
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func trunc(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func writeCSV(w io.Writer, res []Profile) {
	cw := csv.NewWriter(w)
	cw.Write([]string{"name", "headline", "location", "url", "emails", "phones"})
	for _, p := range res {
		cw.Write([]string{p.Name, p.Headline, p.Location, p.URL,
			strings.Join(p.Emails, ";"), strings.Join(p.Phones, ";")})
	}
	cw.Flush()
}

// ---------- Web ----------

func runWeb(args []string) {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	key := fs.String("key", os.Getenv("SERPAPI_KEY"), "default SerpAPI key")
	fs.Parse(args)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, page)
	})
	http.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Q     string `json:"q"`
			Pages int    `json:"pages"`
			Start int    `json:"start"`
			Key   string `json:"key"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		k := req.Key
		if k == "" {
			k = *key
		}
		if req.Pages < 1 || req.Pages > 20 {
			req.Pages = 3
		}
		res, meta, err := search(k, req.Q, req.Start, req.Pages, nil)
		out := map[string]any{
			"results":     res,
			"start_page":  meta.StartPage,
			"last_page":   meta.LastPage,
			"total_pages": meta.TotalPages,
		}
		if err != nil {
			out["error"] = err.Error()
		}
		var buf bytes.Buffer
		json.NewEncoder(&buf).Encode(out)
		w.Header().Set("Content-Type", "application/json")
		w.Write(buf.Bytes())
	})
	fmt.Println("listening on http://" + *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: empenum cli [-q dork] [-start N] [-pages N] [-key K] [-f table|json|csv] [-o file.json]\n       empenum web [-addr host:port] [-key K]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "cli":
		runCLI(os.Args[2:])
	case "web":
		runWeb(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, "unknown mode:", os.Args[1])
		os.Exit(2)
	}
}

//go:embed web/index.html
var page string
