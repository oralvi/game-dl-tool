# game-dl-tool

`game-dl-tool` is a small Windows desktop app for checking CN game CDN routes and applying the result in a practical way.

It can:

- scan multiple DNS resolvers
- collect final `A` and `AAAA` answers after following `CNAME`
- compare TCP latency to CDN endpoints
- write tagged `hosts` entries for selected IPs
- route selected hostnames through a local non-decrypting TCP tunnel
- apply strict download-provider routing for grouped multi-CDN games
- stream scan progress and logs in the GUI
- look up GeoIP and ASN details on demand in `Details` and `Trace`

The app is built with Wails and starts as a GUI only.

## Included targets

The default auto-generated config includes:

- Genshin Impact
- Honkai: Star Rail
- Honkai Impact 3rd
- Zenless Zone Zero
- Wuthering Waves CN
- Girls' Frontline 2: Exilium CN

Each game can map one display name to one or more CDN domains.
For multi-CDN downloaders, a game can also define grouped managed domains and an optional preferred provider.
That lets future games with similar routing behavior be added by editing `config.json` only.

## How it works

The app uses a simple page flow:

- `Domains`
- `Candidates`
- `Details`

You can scan first, then pick one or more candidates and choose one of two routing modes:

- `Direct hosts`: write the selected IP to the system `hosts` file
- `Local tunnel`: map the hostname to loopback and forward raw TLS traffic to the selected upstream IP without decrypting HTTPS

For the same hostname, only one mode is active at a time. Switching modes clears the previous tool-managed entry for that hostname first.

## Config

If `config.json` does not exist, the app creates it automatically on first launch.

Minimal example:

```json
{
  "games": [
    {
      "name": "Genshin Impact",
      "enabled": true,
      "domains": [
        "autopatchcn.yuanshen.com"
      ]
    },
    {
      "name": "Wuthering Waves CN",
      "enabled": true,
      "groups": [
        {
          "name": "Download",
          "mode": "manage",
          "domains": [
            {
              "host": "cdn-aliyun-cn-mc.aki-game.com",
              "provider": "aliyun"
            },
            {
              "host": "cdn-huoshan-cn-mc.aki-game.com",
              "provider": "huoshan"
            },
            {
              "host": "cdn-qcloud-cn-mc.aki-game.com",
              "provider": "qcloud"
            }
          ]
        }
      ],
      "preferred_provider": "auto"
    }
  ],
  "resolvers": [
    "223.5.5.5",
    "223.6.6.6",
    "119.29.29.29",
    "114.114.114.114",
    "180.76.76.76",
    "1.1.1.1",
    "8.8.8.8"
  ],
  "family": "6",
  "geoip": {
    "primary_provider": "ipwho.is",
    "fallback_provider": "ipinfo_lite",
    "ipinfo_token": "",
    "cache_file": "geoip_cache.json",
    "mmdb_city_path": "",
    "mmdb_asn_path": ""
  },
  "tunnel_port": 0
}
```

Notes:

- `family` supports `4`, `6`, or `all`
- `tunnel_port: 0` means the internal tunnel listener uses a random port
- DNS resolvers can be edited in `config.json` or in the app settings
- active adapter DNS servers are always merged automatically and deduped against configured/public resolvers
- each game may use either a simple `domains` list or grouped `groups`
- `groups[].mode: "manage"` marks the download domains that the tool may route or block
- `preferred_provider` supports `auto`, `aliyun`, `huoshan`, `qcloud`, or your own provider labels from grouped domains
- when `preferred_provider` is not `auto`, applying `hosts` or `tunnel` also blocks other managed download providers for that game
- provider selection for grouped games is edited from the game-specific detail dialog inside `Settings`
- this provider preference is intended for download/CDN groups only; telemetry, anti-cheat, SDK, or crash-report domains should stay out of `manage` groups
- hop-by-hop trace runs manually from the `Details` page
- GeoIP lookups run only when you open `Details` or start `Trace`
- GeoIP defaults to `ipwho.is`, with optional `IPinfo Lite` fallback
- Local MMDB files can be configured in the GeoIP settings subpage

## Admin access

Windows builds request administrator privileges at launch.

That is required because the app may:

- update the system `hosts` file
- create loopback portproxy rules for local tunnel routing

The app also keeps two backups next to the system `hosts` file:

- `.game-dl-tool.original.bak`
- `.game-dl-tool.bak`

## Development

Requirements:

- Go `1.26+`
- Node.js and npm
- Wails CLI
- WebView2 runtime on Windows
- MSYS2 UCRT64 `gcc` in `PATH`

Install Wails CLI:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.11.0
```

Run in development:

```powershell
wails dev
```

Build a release locally:

```powershell
.\package.ps1
```

The package script runs tests, builds the frontend, creates a Windows build, and outputs a zip that contains only:

- `game-dl-tool.exe`
- `README.md`

Runtime files created locally:

- `scan_cache.json`
- `geoip_cache.json`
- `latest_scan.log`
