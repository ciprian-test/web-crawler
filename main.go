// Run a crawler
package main

import (
	"os"
	"strings"

	"github.com/ciprian-test/web-crawler/crawler"
)

func main() {
	startURL := os.Getenv("START_URL")
	allowedDomains := os.Getenv("ALLOWED_DOMAINS")

	crawler := crawler.NewCrawler(5)

	if len(allowedDomains) > 0 {
		crawler.SetAllowedDomains(strings.Split(allowedDomains, ","))
	}

	crawler.Crawl(startURL)

	crawler.PrintLinks(false)
}
