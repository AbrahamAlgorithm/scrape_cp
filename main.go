package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

var (
	globalTerms = make(map[string]string)
	mutex       sync.Mutex
)

var sources = []struct {
	URL        string
	Name       string
	ScrapeFunc func(*goquery.Document) map[string]string
}{
	{
		URL:        "https://www.coursera.org/collections/computer-science-terms",
		Name:       "Coursera",
		ScrapeFunc: scrapeCourseraTerms,
	},
	{
		URL:        "https://en.wikipedia.org/wiki/Glossary_of_computer_science",
		Name:       "Wikipedia",
		ScrapeFunc: scrapeWikipediaTerms,
	},
}

func cleanText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, text)
}

func isValidTerm(term, definition string) bool {
	if len(term) < 2 || len(definition) < 10 {
		return false
	}

	termForComparison := term
	if i := strings.Index(term, " ("); i != -1 {
		termForComparison = term[:i]
	}

	if strings.Contains(strings.ToLower(definition), strings.ToLower(termForComparison)) &&
		len(definition) < len(termForComparison)+20 {
		return false
	}

	return true
}

func scrapeWikipediaTerms(doc *goquery.Document) map[string]string {
	terms := make(map[string]string)

	doc.Find("dl.glossary").Each(func(i int, dlElement *goquery.Selection) {
		var currentTerm string

		dlElement.Children().Each(func(j int, element *goquery.Selection) {
			if element.Is("dt") {
				currentTerm = cleanText(element.Text())
				currentTerm = strings.Split(currentTerm, "[")[0]
				currentTerm = strings.TrimSpace(currentTerm)
			} else if element.Is("dd") && currentTerm != "" {
				definition := cleanText(element.Text())

				definition = strings.Map(func(r rune) rune {
					if r == '[' || r == ']' {
						return -1
					}
					return r
				}, definition)

				definition = strings.Split(definition, "[")[0]
				definition = strings.TrimSpace(definition)

				if isValidTerm(currentTerm, definition) {
					terms[currentTerm] = definition
				}
			}
		})
	})

	return terms
}

func scrapeCourseraTerms(doc *goquery.Document) map[string]string {
	terms := make(map[string]string)

	doc.Find("p").Each(func(i int, s *goquery.Selection) {
		if strong := s.Find("strong"); strong.Length() > 0 {
			term := cleanText(strong.Text())
			if nextP := s.Next(); nextP.Length() > 0 {
				definition := cleanText(nextP.Text())
				if isValidTerm(term, definition) {
					terms[term] = definition
				}
			}
		}
	})

	return terms
}

func scrapeURL(url string, scrapeFunc func(*goquery.Document) map[string]string, wg *sync.WaitGroup) {
	defer wg.Done()

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Error creating request for %s: %v", url, err)
		return
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to fetch %s: %v", url, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Bad status code %d from %s", resp.StatusCode, url)
		return
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Printf("Failed to parse HTML from %s: %v", url, err)
		return
	}

	terms := scrapeFunc(doc)

	mutex.Lock()
	for term, def := range terms {
		if existing, exists := globalTerms[term]; !exists ||
			len(def) > len(existing) {
			globalTerms[term] = def
		}
	}
	mutex.Unlock()
}

func main() {
	var wg sync.WaitGroup

	os.MkdirAll("output", 0755)

	for _, source := range sources {
		wg.Add(1)
		go scrapeURL(source.URL, source.ScrapeFunc, &wg)
	}

	wg.Wait()

	if len(globalTerms) == 0 {
		log.Fatal("No terms were found from any source")
	}

	jsonData, err := json.MarshalIndent(globalTerms, "", "    ")
	if err != nil {
		log.Fatal("Failed to convert to JSON:", err)
	}

	timestamp := time.Now().Format("2025-01-02_15-04-05")
	filename := fmt.Sprintf("output/cs_terms_%s.json", timestamp)

	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		log.Fatal("Failed to write file:", err)
	}

	fmt.Printf("Successfully scraped %d unique terms and saved to %s\n", len(globalTerms), filename)
}
