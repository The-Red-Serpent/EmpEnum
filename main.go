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
	Error   string `json:"error"`
	Organic []struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
	} `json:"organic_results"`
}

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

func search(key, q string, pages int) ([]Profile, error) {
	if key == "" {
		return nil, fmt.Errorf("missing SerpAPI key")
	}
	seen := map[string]bool{}
	var out []Profile
	for pg := 0; pg < pages; pg++ {
		v := url.Values{
			"engine":  {"google"},
			"q":       {q},
			"start":   {strconv.Itoa(pg * 10)},
			"api_key": {key},
		}
		resp, err := client.Get("https://serpapi.com/search?" + v.Encode())
		if err != nil {
			return out, err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var sr serpResp
		if err := json.Unmarshal(body, &sr); err != nil {
			return out, fmt.Errorf("bad response (HTTP %d): %w", resp.StatusCode, err)
		}
		if sr.Error != "" {
			// SerpAPI says this when a page has no more results
			if len(out) > 0 && strings.Contains(strings.ToLower(sr.Error), "hasn't returned any results") {
				break
			}
			return out, fmt.Errorf("serpapi: %s", sr.Error)
		}
		if len(sr.Organic) == 0 {
			break
		}
		for _, r := range sr.Organic {
			if !strings.Contains(r.Link, "linkedin.com/in/") || seen[r.Link] {
				continue
			}
			seen[r.Link] = true
			out = append(out, parse(r.Title, r.Link, r.Snippet))
		}
		time.Sleep(500 * time.Millisecond)
	}
	return out, nil
}

// ---------- CLI ----------

func runCLI(args []string) {
	fs := flag.NewFlagSet("cli", flag.ExitOnError)
	q := fs.String("q", `site:linkedin.com/in/ "Marriott International"`, "dork")
	pages := fs.Int("pages", 3, "result pages (10 per page)")
	key := fs.String("key", os.Getenv("SERPAPI_KEY"), "SerpAPI key (or SERPAPI_KEY env)")
	format := fs.String("f", "table", "table | json | csv")
	out := fs.String("o", "", "write results to this JSON file (suppresses stdout unless -f is set)")
	fs.Parse(args)

	res, err := search(*key, *q, *pages)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		if len(res) == 0 {
			os.Exit(1)
		}
	}
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
			return
		}
	}
	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(res)
	case "csv":
		writeCSV(os.Stdout, res)
	default:
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
		res, err := search(k, req.Q, req.Pages)
		out := map[string]any{"results": res}
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
		fmt.Fprintln(os.Stderr, "usage: dorkscrape cli [-q dork] [-pages N] [-key K] [-f table|json|csv]\n       dorkscrape web [-addr host:port] [-key K]")
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
