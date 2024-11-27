// Package crawler Crawl a website responsibly
package crawler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

var findMetaRefreshRegexp = regexp.MustCompile(`<meta\s+http-equiv=["']?refresh["']?\s+content=["']?[^;]+;\s*url=([^"']+)["']?`)
var findLinkRegexp = regexp.MustCompile(`https?://[^\s"']+`)

// Crawler main structure for the crawler
type Crawler struct {
	allowedDomains  []string       // Only crawl links from these domains
	semaphore       chan struct{}  // Limit concurrency
	mutex           sync.Mutex     // Synchronize access to crawled links data
	wait            sync.WaitGroup // Wait for all routines to finish
	discoveredLinks map[string]map[string]string
}

// NewCrawler creates a new Crawler with a max concurrency limit to avoid damaging the crawler website(s)
func NewCrawler(maxConcurrency int) *Crawler {
	return &Crawler{
		allowedDomains:  []string{},
		discoveredLinks: make(map[string]map[string]string),
		semaphore:       make(chan struct{}, maxConcurrency),
	}
}

// SetAllowedDomains prevent crawling unwanted domains
func (c *Crawler) SetAllowedDomains(allowedDomains []string) {
	c.allowedDomains = allowedDomains
}

// Crawl - Start crawling
func (c *Crawler) Crawl(startURL string) {
	c.wait.Add(1)

	go c.crawlURL(startURL)

	c.wait.Wait()
	close(c.semaphore) // Close the semaphore when done
}

// PrintLinks print to the discovered links
func (c *Crawler) PrintLinks(includeDetails bool) {
	links := make([]string, 0, len(c.discoveredLinks))
	for link := range c.discoveredLinks {
		links = append(links, link)
	}

	sort.Strings(links)

	for _, link := range links {
		details := c.discoveredLinks[link]

		fmt.Println(link)

		if !includeDetails {
			continue
		}

		if val, ok := details["newLocation"]; ok {
			fmt.Printf("\tRedirects to: %s\n", val)
		}

		if val, ok := details["err"]; ok {
			fmt.Printf("\tError detected: %s\n", val)
		}
	}

	fmt.Printf("Found %d unique links", len(links))
}

func (c *Crawler) crawlURL(link string) {
	defer c.wait.Done()

	// Acquire a slot
	c.semaphore <- struct{}{}
	defer func() { <-c.semaphore }()

	c.mutex.Lock()

	if _, ok := c.discoveredLinks[link]; ok {
		c.mutex.Unlock()
		return
	}

	linkDetails := map[string]string{}

	baseURL, err := url.Parse(link)
	if err != nil || !c.isDomainAllowed(baseURL.Host) {
		c.mutex.Unlock()
		return
	}

	c.discoveredLinks[link] = linkDetails
	c.mutex.Unlock()

	body, contentType, location, err := c.getURL(link)
	if err != nil {
		linkDetails["err"] = err.Error()
		return
	}

	links := []string{}

	linkDetails["contentType"] = contentType
	if len(location) > 0 {
		newLocationURL := resolveURL(location, baseURL)
		linkDetails["newLocation"] = newLocationURL

		if c.isLocationAllowed(newLocationURL) {
			links = append(links, newLocationURL)
		}
	} else {
		linksDetails, err := c.extractLinks(baseURL, body, contentType)
		if err != nil {
			fmt.Println(err)
			return
		}

		c.mutex.Lock()
		for link, needsCrawling := range linksDetails {
			if needsCrawling {
				links = append(links, link)
			} else if c.isDomainAllowedForLink(link) {
				c.discoveredLinks[link] = map[string]string{}
			}
		}
		c.mutex.Unlock()

		links = uniqueStrings(links)
	}

	for _, newLink := range links {
		c.wait.Add(1)
		go c.crawlURL(newLink)
	}
}

func (c *Crawler) getURL(link string) (string, string, string, error) {
	// Create a custom HTTP client that does not follow redirects
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Return an error to prevent following redirects
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", link, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")

	if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusPermanentRedirect {
		location := resp.Header.Get("Location")
		return "", contentType, location, nil
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("%d status code", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading body:", err)
		return "", "", "", fmt.Errorf("Error reading URL body (%s)", err)
	}

	return string(body), contentType, "", nil
}

// Extract links from the URL body
func (c *Crawler) extractLinks(baseURL *url.URL, body string, contentType string) (map[string]bool, error) {
	if strings.Contains(contentType, "/javascript") || strings.Contains(contentType, "/css") {
		// Look for links in certain file types
		return extractLinksFromFile(body, baseURL), nil
	}

	linksDetails, err := c.extractLinksFromHTML(baseURL, body)
	if err != nil {
		return nil, err
	}

	// Extract from meta refresh
	if metaRefresh := extractMetaRefresh(body); metaRefresh != "" {
		linksDetails[resolveURL(metaRefresh, baseURL)] = true
	}

	return linksDetails, nil
}

// Extract links from the URL body
func (c *Crawler) extractLinksFromHTML(baseURL *url.URL, body string) (map[string]bool, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("Error parsing HTML body (%s)", err)
	}

	newLinks := map[string]bool{}

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "img":
				val := getAttributeValue(n, []string{"src"})
				if len(val) > 0 {
					newLinks[resolveURL(val, baseURL)] = false
				}

			case "a", "link", "iframe", "embed", "object", "source", "script":
				val := getAttributeValue(n, []string{"src", "href"})
				if len(val) > 0 {
					newLinks[resolveURL(val, baseURL)] = true
				}
			case "form":
				val := getAttributeValue(n, []string{"action"})
				if len(val) > 0 {
					newLinks[resolveURL(val, baseURL)] = false
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			traverse(child)
		}
	}
	traverse(doc)

	return newLinks, nil
}

func (c *Crawler) isDomainAllowed(domain string) bool {
	for _, allowedDomain := range c.allowedDomains {
		if allowedDomain == domain || strings.Index(domain, "."+allowedDomain) > 0 {
			return true
		}
	}

	return false
}

func (c *Crawler) isDomainAllowedForLink(link string) bool {
	linkURL, err := url.Parse(link)
	if err != nil {
		return false
	}

	return c.isDomainAllowed(linkURL.Host)
}

func (c *Crawler) isLocationAllowed(location string) bool {
	locationURL, err := url.Parse(location)
	if err != nil {
		return false
	}

	return c.isDomainAllowed(locationURL.Host)
}

func resolveURL(link string, baseURL *url.URL) string {
	parsed, err := baseURL.Parse(link)
	if err != nil {
		return ""
	}

	parsed.Fragment = ""

	return parsed.String()
}

func uniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	unique := []string{}

	for _, str := range input {
		if _, exists := seen[str]; !exists {
			seen[str] = true
			unique = append(unique, str)
		}
	}

	return unique
}

func getAttributeValue(n *html.Node, keys []string) string {
	for _, attr := range n.Attr {
		for _, key := range keys {
			if attr.Key == key {
				return attr.Val
			}
		}
	}

	return ""
}

func extractMetaRefresh(body string) string {
	matches := findMetaRefreshRegexp.FindStringSubmatch(body)
	if len(matches) > 1 {
		return matches[1]
	}

	return ""
}

func extractLinksFromFile(body string, baseURL *url.URL) map[string]bool {
	matches := findLinkRegexp.FindAllString(body, -1)

	newLinks := map[string]bool{}

	for _, match := range matches {
		newLinks[resolveURL(match, baseURL)] = true
	}

	return newLinks
}
