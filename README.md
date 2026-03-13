# game-dl-tool

Interactive Go CLI for scanning game CDN domains, comparing IPv4 and IPv6 candidates, and optionally appending tagged entries to `hosts`.

Built-in game menu:

1. `原神`
2. `星铁`
3. `崩坏3`
4. `绝区零`
5. `鸣潮国服`

Built-in domains include:

- `autopatchcn.yuanshen.com`
- `autopatchcn.bhsr.com`
- `autopatchcn.bh3.com`
- `autopatchcn.juequling.com`
- `prod-cn-alicdn-gamestarter.kurogame.com`
- `pcdownload-aliyun.aki-game.com`
- `pcdownload-huoshan.aki-game.com`
- `pcdownload-qcloud.aki-game.com`

What it does:

- prompts for game selection when you run it without domains
- supports `-family 4`, `-family 6`, and `-family all`
- samples multiple DNS resolvers and unions the answers
- follows `CNAME` chains to final `A` or `AAAA` answers
- measures TCP latency, default port `443`
- can run `tracert` or `traceroute` when `-trace` is enabled
- can load custom domains and defaults from `config.json`
- saves scan cache to `scan_cache.json`
- can load cached results with `-use-cache`
- shows numbered best candidates and can append selected tagged hosts entries like `#DLTOOL`
- shows a live progress bar while a scan is running
- writes a run log to `cache/latest_scan.log` by default

Quick start:

```powershell
cd d:\projects\reverse-analysis-workspace\projects\game-dl-tool
go run .
```

Config example:

```json
{
  "domains": [
    "autopatchcn.bh3.com",
    "autopatchcn.bhsr.com",
    "autopatchcn.yuanshen.com",
    "autopatchcn.juequling.com",
    "prod-cn-alicdn-gamestarter.kurogame.com",
    "pcdownload-aliyun.aki-game.com",
    "pcdownload-huoshan.aki-game.com",
    "pcdownload-qcloud.aki-game.com"
  ],
  "family": "6",
  "trace": false
}
```

Non-interactive examples:

```powershell
go run . -games 12 -family all -csv result.csv
go run . -games 1 -family 6 -hosts-out system
go run . -config .\config.json
go run . -input .\domains.txt -family all -use-cache
```

Packaging:

```powershell
.\package.ps1
```

Notes:

- Running without domains uses the interactive menu. Press Enter to accept defaults.
- If `config.json` does not exist, the program will auto-create one with your default CN domains and `family: "6"`.
- If `config.json` exists and contains `domains` or default options, it will be applied before the interactive menu.
- Trace probing is disabled by default to keep scans responsive. In interactive mode you can enable it when prompted, or use `-trace`.
- After the result table is printed, you can choose candidate IDs to append to the system `hosts`; press Enter to skip.
- Cached results are saved automatically after a live scan.
- Hosts updates only replace tagged `#DLTOOL` lines for the hostnames selected in the current write; other lines are left untouched.
- For Genshin hosts updates, the best result for `autopatchcn.yuanshen.com` is also written to:
  - `autopatchhk.yuanshen.com`
  - `genshinimpact.mihoyo.com`
