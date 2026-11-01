package main

import (
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard"

	_ "github.com/caddy-dns/alidns"
	_ "github.com/caddy-dns/cloudflare"
	_ "github.com/caddy-dns/digitalocean"
	_ "github.com/caddy-dns/godaddy"
	_ "github.com/caddy-dns/namecheap"
	_ "github.com/caddy-dns/tencentcloud"
	_ "github.com/caddy-dns/vultr"
	_ "github.com/mholt/caddy-l4"
	_ "github.com/mholt/caddy-ratelimit"
)

func main() { caddycmd.Main() }
