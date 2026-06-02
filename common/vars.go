package common

var (
	// App name
	App = "rotasiIP"
	// Version of rotasiIP itself
	Version = ""
	// Author
	Author = "@dwisiswant0"
	// Banner
	Banner = `
  _____     _           _ ___ ___
 | __  |___| |_ ___ ___|_|  _|  _|
 |    -| . |  _| .'|_ -| |  _|  _|
 |__|__|___|_| |__,|___|_|_| |_|
  by ` + Author
	// Usage
	Usage = `
  rotasiIP [-c|-a :8080] -f file.txt [options...]

Options:
  GENERAL
    -f, --file <FILE>                Proxy file (required)
    -o, --output <FILE>              Write log output to FILE
    -t, --timeout <TIME>             Max. time allowed for connection (default: 30s)
    -u, --update                     Update rotasiIP to the latest stable version
    -v, --verbose                    Verbose mode
    -V, --version                    Show current rotasiIP version
        --max-retries <N>            Max. retries for failed HTTP requests (default: 0)

  PROXY CHECKER
    -c, --check                      Perform proxy check
    -g, --goroutine <N>              Max. goroutine to use (default: 50)
        --only-cc <AA>,<BB>          Only for specific country code (comma separated)
        --output-format <FORMAT>     Custom output format using fasttemplate syntax
                                     Available variables: {{proxy}}, {{protocol}},
                                     {{host}}, {{port}}, {{ip}}, {{country}},
                                     {{city}}, {{org}}, {{region}}, {{timezone}},
                                     {{loc}}, {{hostname}}, {{duration}}

  IP ROTATOR
    -a, --address <ADDR>:<PORT>      Run proxy server
    -A, --auth <USER>:<PASS>         Set authorization for proxy server
    -d, --daemon                     Daemonize proxy server
    -m, --method <METHOD>            Rotation method (sequent/random) (default: sequent)
    -r, --rotate <N>                 Rotate proxy IP after N request (default: 1)
        --rotate-on-error            Rotate proxy IP and retry failed HTTP requests
        --remove-on-error            Remove proxy IP from proxy pool on failed HTTP requests
        --max-errors <N>             Max. errors allowed during rotation (default: 3)
                                     Use this with --rotate-on-error
                                     If value is less than 0 (e.g., -1), rotation will
                                     continue indefinitely
        --max-redirs <N>             Max. redirects allowed (default: 10)
        --delay <DURATION>           Fixed delay between requests (e.g. 500ms)
        --rate-limit <N>             Max requests per second
        --retry-on-status <CODES>    Rotate and retry on HTTP status codes (e.g. 429,503)
        --proxy-cc <AA>,<BB>         Only load proxies with matching #CC suffix
    -s, --sync                       Synchronous mode
    -w, --watch                      Watch proxy file, live-reload from changes
        --ui <ADDR>:<PORT>           Start web dashboard (e.g. 0.0.0.0:9191)

Examples:
  rotasiIP -f proxies.txt --check --output live.txt
  rotasiIP -f proxies.txt --check --output-format "{{proxy}}#{{country}}"
  rotasiIP -a localhost:8080 -f live.txt -r 10 -m random --ui 0.0.0.0:9191
  rotasiIP -a localhost:8080 -f live.txt --delay 500ms --retry-on-status 429,503

`
)
