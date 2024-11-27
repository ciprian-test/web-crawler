package crawler

import (
	"net/url"
	"testing"
)

func TestCrawl(t *testing.T) {
	server := mockServer()
	defer server.Close()

	c := NewCrawler(5)

	// Start crawling from the root
	c.Crawl(server.URL + "/")

	// Check if discovered links contain expected pages
	expectedLinks := []string{
		server.URL + "/",
		server.URL + "/page1",
		server.URL + "/page2",
		server.URL + "/page3",
		server.URL + "/image.jpg",
	}

	for _, link := range expectedLinks {
		if _, found := c.discoveredLinks[link]; !found {
			t.Errorf("Expected link not found: %s", link)
		}
	}
}

func TestRedirectHandling(t *testing.T) {
	server := mockServer()
	defer server.Close()

	c := NewCrawler(5)

	c.Crawl(server.URL + "/redirect")

	// Ensure redirect was followed
	if _, found := c.discoveredLinks[server.URL+"/"]; !found {
		t.Errorf("Redirected link not followed: %s", server.URL+"/")
	}
}

func TestErrorHandling(t *testing.T) {
	server := mockServer()
	defer server.Close()

	c := NewCrawler(5)

	c.Crawl(server.URL + "/error")

	// Ensure error was recorded
	if details, found := c.discoveredLinks[server.URL+"/error"]; found {
		if _, errRecorded := details["err"]; !errRecorded {
			t.Errorf("Expected error not recorded for %s", server.URL+"/error")
		}
	} else {
		t.Errorf("Error link not discovered: %s", server.URL+"/error")
	}
}

func TestExtractLinks(t *testing.T) {
	c := NewCrawler(5)
	baseURL, _ := url.Parse("http://example.com")

	body := `
		<html>
			<body>
				<a href="/page1">Page 1</a>
				<a href="http://external.com/page2">External Page</a>
			</body>
		</html>
	`

	links, err := c.extractLinks(baseURL, body, "text/html")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expectedLinks := map[string]bool{
		"http://example.com/page1":  true,
		"http://external.com/page2": true,
	}

	for link, needsCrawling := range expectedLinks {
		if v, found := links[link]; !found || v != needsCrawling {
			t.Errorf("Expected link %s with crawling: %v not found or mismatched", link, needsCrawling)
		}
	}
}
