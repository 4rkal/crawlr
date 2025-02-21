package main

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

var mu sync.Mutex

func GetStatusCode(url string) int {
	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("Error:", err)
		return 0
	}
	defer resp.Body.Close()

	fmt.Println("Status Code:", resp.StatusCode)
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
		fmt.Println("Error parsing link:", err)
		return false
	}
	return parsedLink.Host == base.Host
}

func sanitizeURL(link string) string {
	parsedURL, err := url.Parse(link)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
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
		fmt.Println("Error parsing URL:", err)
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
		fmt.Println("Error parsing link:", err)
		return false
	}
	return parsedLink.Fragment != ""
}

func crawl(BaseUrlStr string, visited map[string]bool, wg *sync.WaitGroup) {
	defer wg.Done()
	BaseUrl, err := url.Parse(BaseUrlStr)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return
	}
	resp, err := http.Get(BaseUrlStr)
	if err != nil {
		fmt.Println("Error fetching the page:", err)
		return
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	if err != nil {
		fmt.Println("Error parsing HTML:", err)
		return
	}

	links := extractLinks(doc)
	for _, link := range links {
		if isFragmentLink(link) {
			// fmt.Println("Skipping fragment link:", link)
			continue
		}

		resolvedURL := resolveURL(link, BaseUrl)
		fmt.Println(link)

		mu.Lock()
		if visited[resolvedURL] {
			mu.Unlock()
			continue
		}
		visited[resolvedURL] = true
		mu.Unlock()

		if isSitemap(resolvedURL) {
			fmt.Println("Skipping sitemap:", resolvedURL)
			continue
		}

		if isSameDomain(link, BaseUrl) {
			code := GetStatusCode(resolvedURL)
			appendToCSV(BaseUrlStr, resolvedURL, code)

			if code == 200 {
				fmt.Println("URL is valid")
				if link != BaseUrlStr {
					wg.Add(1)
					go crawl(link, visited, wg)
				}
			} else {
				fmt.Println("URL is invalid")
			}
		} else {
			code := GetStatusCode(resolvedURL)
			appendToCSV(BaseUrlStr, resolvedURL, code)

			if code == 200 {
				fmt.Println("URL is valid")
			} else {
				fmt.Println("URL is invalid")
			}
		}
	}
}

func main() {
	var BaseUrlStr string
	fmt.Print("Enter website url: ")
	fmt.Scan(&BaseUrlStr)

	visited := make(map[string]bool)
	var wg sync.WaitGroup

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
		err = writer.Write([]string{"Current Page URL", "Linked URL", "Status Code"})
		if err != nil {
			fmt.Println("Error writing header to CSV:", err)
			return
		}
	}

	wg.Add(1)
	go crawl(BaseUrlStr, visited, &wg)

	wg.Wait()

}
