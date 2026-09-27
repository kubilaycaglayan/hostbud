package bad

import "net/http"

func run() { _ = (&http.Client{}).Timeout }
