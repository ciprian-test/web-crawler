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
	discoveredLinks map[string]bool
}

// NewCrawler creates a new Crawler with a max concurrency limit to avoid damaging the crawler website(s)
func NewCrawler(maxConcurrency int) *Crawler {
	return &Crawler{
		allowedDomains:  []string{},
		discoveredLinks: make(map[string]bool),
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
		fmt.Println(link)
	}

	fmt.Printf("Found %d unique links", len(links))
}

func (c *Crawler) crawlURL(link string) {
	defer c.wait.Done()

	// Acquire a slot
	c.semaphore <- struct{}{}
	defer func() { <-c.semaphore }()

	c.mutex.Lock()

	if c.discoveredLinks[link] {
		c.mutex.Unlock()
		return
	}

	c.discoveredLinks[link] = true
	c.mutex.Unlock()

	baseURL, err := url.Parse(link)
	if err != nil {
		fmt.Printf("Error parsing URL (%s): %s", link, err)
		return
	}

	body, err := c.getURL(link)
	if err != nil {
		fmt.Println(err)
		return
	}

	links, err := c.extractLinks(baseURL, body)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, newLink := range links {
		c.wait.Add(1)
		go c.crawlURL(newLink)
	}
}

func (c *Crawler) getURL(link string) (string, error) {
	resp, err := http.Get(link)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Unexpected status code (%d) for %s", resp.StatusCode, link)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading body:", err)
		return "", fmt.Errorf("Error reading URL body (%s)", err)
	}

	return string(body), nil
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
