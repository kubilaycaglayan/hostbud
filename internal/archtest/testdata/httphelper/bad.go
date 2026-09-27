package bad

import "net/http"

func run() { _, _ = http.Get("https://example.com") }
