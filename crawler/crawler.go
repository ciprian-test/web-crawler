// Package crawler Crawl a website responsibly
package crawler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

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
func (c *Crawler) PrintLinks() {
	links := make([]string, 0, len(c.discoveredLinks))
	for link := range c.discoveredLinks {
		links = append(links, link)
	}

	sort.Strings(links)

	for _, link := range links {
		details := c.discoveredLinks[link]

		fmt.Println(link)

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

	c.discoveredLinks[link] = linkDetails
	c.mutex.Unlock()

	baseURL, err := url.Parse(link)
	if err != nil {
		linkDetails["err"] = fmt.Sprintf("Error parsing URL (%s)", err)
		return
	}

	body, contentType, location, err := c.getURL(link)
	if err != nil {
		linkDetails["err"] = err.Error()
		return
	}

	links := []string{}

	linkDetails["contentType"] = contentType
	if len(location) > 0 {
		newLocationURL, err := baseURL.Parse(location)
		if err != nil {
			return
		}

		linkDetails["newLocation"] = newLocationURL.String()

		if c.isLocationAllowed(location) {
			links = append(links, location)
		}
	} else {
		links, err = c.extractLinks(baseURL, body)
		if err != nil {
			fmt.Println(err)
			return
		}
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
func (c *Crawler) extractLinks(baseURL *url.URL, body string) ([]string, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("Error parsing HTML body (%s)", err)
	}

	var newLinks []string

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					newURL, err := baseURL.Parse(attr.Val)
					if err != nil || !c.isDomainAllowed(newURL.Host) {
						continue
					}
					newURL.Fragment = ""
					newLinks = append(newLinks, newURL.String())
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

func (c *Crawler) isLocationAllowed(location string) bool {
	locationURL, err := url.Parse(location)
	if err != nil {
		return false
	}

	return c.isDomainAllowed(locationURL.Host)
}
