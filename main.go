package main

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/net/html"
)

var mu sync.Mutex
var broken, valid, total int
var done bool

var updateChan = make(chan struct{})

type model struct{}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" {
			return m, tea.Quit
		}
	}

	if done { // When the crawl is finished
		return m, tea.Quit // Gracefully quit the TUI
	}

	return m, nil
}
func (m model) View() string {
	// Color codes
	red := "\033[31m"
	green := "\033[32m"
	reset := "\033[0m"

	validStr := fmt.Sprintf("%s%d%s", green, valid, reset)
	brokenStr := fmt.Sprintf("%s%d%s", red, broken, reset)

	// Show status or done message
	if done {
		return fmt.Sprintf("Crawl finished!\nTotal links: %d\nValid links: %s\nBroken links: %s\n", total, validStr, brokenStr)
	}

	return fmt.Sprintf("Total links: %d\nValid links: %s\nBroken links: %s\n(Press 'q' to quit)\n", total, validStr, brokenStr)
}

func GetStatusCode(url string) int {
	resp, err := http.Get(url)
	if err != nil {
		// fmt.Println("Error:", err)
		return 0
	}
	defer resp.Body.Close()

	// fmt.Println("Status Code:", resp.StatusCode)
	return resp.StatusCode
}

func extractLinks(n *html.Node) []string {
	var links []string

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					links = append(links, attr.Val)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}

	traverse(n)
	return links
}

func isSameDomain(link string, base *url.URL) bool {
	parsedLink, err := url.Parse(link)
	if err != nil {
		// fmt.Println("Error parsing link:", err)
		return false
	}
	return parsedLink.Host == base.Host
}

func sanitizeURL(link string) string {
	parsedURL, err := url.Parse(link)
	if err != nil {
		// fmt.Println("Error parsing URL:", err)
		return link
	}

	queryParams := parsedURL.Query()

	removeParams := []string{"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content"}

	for _, param := range removeParams {
		queryParams.Del(param)
	}

	parsedURL.RawQuery = queryParams.Encode()

	return parsedURL.String()
}

func resolveURL(link string, base *url.URL) string {
	parsedURL, err := url.Parse(link)
	if err != nil {
		// fmt.Println("Error parsing URL:", err)
		return link
	}

	resolved := base.ResolveReference(parsedURL)
	resolved.Fragment = ""
	return sanitizeURL(resolved.String())
}

func isSitemap(link string) bool {
	lowerLink := strings.ToLower(link)
	return strings.Contains(lowerLink, "sitemap") || strings.HasSuffix(lowerLink, ".xml") || strings.HasSuffix(lowerLink, "robots.txt")
}

func appendToCSV(currentPage, url string, statusCode int) {
	file, err := os.OpenFile("urls.csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening CSV file:", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	err = writer.Write([]string{currentPage, url, fmt.Sprintf("%d", statusCode)})
	if err != nil {
		fmt.Println("Error writing record to CSV:", err)
	}
}

func isFragmentLink(link string) bool {
	parsedLink, err := url.Parse(link)
	if err != nil {
		// fmt.Println("Error parsing link:", err)
		return false
	}
	return parsedLink.Fragment != ""
}

func crawl(BaseUrlStr string, visited map[string]bool, wg *sync.WaitGroup) {
	defer wg.Done()
	BaseUrl, err := url.Parse(BaseUrlStr)
	if err != nil {
		// fmt.Println("Error parsing URL:", err)
		return
	}
	resp, err := http.Get(BaseUrlStr)
	if err != nil {
		// fmt.Println("Error fetching the page:", err)
		return
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	if err != nil {
		// fmt.Println("Error parsing HTML:", err)
		return
	}

	links := extractLinks(doc)
	for _, link := range links {
		if isFragmentLink(link) {
			// fmt.Println("Skipping fragment link:", link)
			continue
		}

		resolvedURL := resolveURL(link, BaseUrl)
		// fmt.Println(link)

		mu.Lock()
		if visited[resolvedURL] {
			mu.Unlock()
			continue
		}
		visited[resolvedURL] = true
		mu.Unlock()

		if isSitemap(resolvedURL) {
			// fmt.Println("Skipping sitemap:", resolvedURL)
			continue
		}

		total++

		if isSameDomain(link, BaseUrl) {
			code := GetStatusCode(resolvedURL)
			appendToCSV(BaseUrlStr, resolvedURL, code)

			if code == 200 {
				// fmt.Println("URL is valid")
				valid++
				if link != BaseUrlStr {
					wg.Add(1)
					go crawl(link, visited, wg)
				}
			} else {
				broken++
				// fmt.Println("URL is invalid")
			}
			updateChan <- struct{}{}

		} else {
			code := GetStatusCode(resolvedURL)
			appendToCSV(BaseUrlStr, resolvedURL, code)

			if code == 200 {
				valid++
				// fmt.Println("URL is valid")
			} else {
				broken++
				// fmt.Println("URL is invalid")
			}
			updateChan <- struct{}{}

		}
	}
	// done = true
}

func main() {
	var BaseUrlStr string
	fmt.Print("Enter website url: ")
	fmt.Scan(&BaseUrlStr)

	visited := make(map[string]bool)
	var wg sync.WaitGroup
	broken, valid, total = 0, 0, 0

	file, err := os.OpenFile("urls.csv", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Println("Error opening CSV file:", err)
		return
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()

	stat, err := file.Stat()
	if err != nil {
		fmt.Println("Error checking file:", err)
		return
	}

	if stat.Size() == 0 {
		_ = writer.Write([]string{"Current Page URL", "Linked URL", "Status Code"})
	}
	wg.Add(1)
	go crawl(BaseUrlStr, visited, &wg)

	p := tea.NewProgram(model{})
	go func() {
		for range updateChan {
			p.Send(struct{}{})
		}

	}()

	if err := p.Start(); err != nil {
		fmt.Println("Error running TUI:", err)
		os.Exit(1)
	}

}
