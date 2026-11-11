# EmpEnum

**Employee Enumeration through OSINT & Google Dorking**

EmpEnum is a small OSINT tool that uses Google Dorks and SerpAPI to discover publicly indexed LinkedIn employee profiles associated with a specific organization.

It extracts profile names and, where available, email addresses or phone numbers appearing in search result snippets.

The project was built to solve a personal problem I had when performing organization-level reconnaissance. It's not intended to be some groundbreaking cybersecurity tool or a polished commercial product — just a simple utility that gets the job done.

**Built with AI assistance.**

## Features

- Google Dork-based LinkedIn profile discovery.
- SERP API integration for retrieving search results.
- Extraction of employee names from indexed profiles.
- Detection of email addresses and phone numbers appearing in search snippets.
- CSV, JSON, and table output formats.
- Web UI and CLI support.
- Docker support for easy deployment.

## Docker (Web UI)

    export SERPAPI_KEY=...
    docker compose up --build -d

Open: http://127.0.0.1:8080

## Docker (CLI)

    docker build -t empenum .

    docker run --rm -e SERPAPI_KEY empenum cli -pages 5 -f csv > out.csv

    docker run --rm -e SERPAPI_KEY empenum cli -q 'site:linkedin.com/in/ "Marriott International" "manager"' -f json

## Native Installation

    go build -o empenum .

Run the CLI:

    ./empenum cli -f table

Run the web interface:

    ./empenum web -addr 127.0.0.1:8080

## Available Flags

### CLI

| Flag | Description |
|---|---|
| `-q` | Google Dork search query |
| `-pages` | Number of search result pages |
| `-key` | SerpAPI API key |
| `-f` | Output format: table, JSON, or CSV |

### Web

| Flag | Description |
|---|---|
| `-addr` | Server address |
| `-key` | SerpAPI API key |

## Disclaimer

EmpEnum is an experimental personal project created for learning and authorized OSINT research. It was developed with AI assistance. It is not a sophisticated or production-grade reconnaissance framework. It's simply a tool I put together to solve a problem I encountered in my own workflow.
