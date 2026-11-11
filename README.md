# dorkscrape

Google dork -> LinkedIn profile names (and any email/phone in the snippet) via SerpAPI.

## Docker (web UI)
    export SERPAPI_KEY=...
    docker compose up --build -d      # http://127.0.0.1:8080

## Docker (CLI)
    docker build -t dorkscrape .
    docker run --rm -e SERPAPI_KEY dorkscrape cli -pages 5 -f csv > out.csv
    docker run --rm -e SERPAPI_KEY dorkscrape cli -q 'site:linkedin.com/in/ "Marriott International" "manager"' -f json

## Native
    go build -o dorkscrape .
    ./dorkscrape cli -f table
    ./dorkscrape web -addr 127.0.0.1:8080

Flags (cli): -q dork, -pages N, -key K, -f table|json|csv
Flags (web): -addr host:port, -key K
