# game-dl-tool

Desktop-first Fyne app for scanning CN game CDN domains, comparing IPv4 and IPv6 candidates, and optionally writing tagged `hosts` entries.

Built-in targets:

- `autopatchcn.yuanshen.com`
- `autopatchcn.bhsr.com`
- `autopatchcn.bh3.com`
- `autopatchcn.juequling.com`
- `prod-cn-alicdn-gamestarter.kurogame.com`
- `pcdownload-aliyun.aki-game.com`
- `pcdownload-huoshan.aki-game.com`
- `pcdownload-qcloud.aki-game.com`

What it does:

- launches a desktop GUI by default
- keeps a CLI fallback with `-cli`
- supports `-family 4`, `-family 6`, and `-family all`
- samples multiple DNS resolvers and unions the answers
- follows `CNAME` chains to final `A` or `AAAA` answers
- measures TCP latency on port `443` by default
- can run `tracert` or `traceroute` when trace is enabled
- auto-creates `config.json` when it does not exist
- saves scan cache to `scan_cache.json`
- can load cached results instead of running a live scan
- writes tagged `#DLTOOL` hosts entries without touching unrelated lines
- streams progress and log output during long scans

Quick start:

```powershell
cd d:\projects\reverse-analysis-workspace\projects\game-dl-tool
go run .
```

CLI fallback:

```powershell
go run . -cli -games 12 -family all -csv result.csv
go run . -cli -games 1 -family 6 -hosts-out system
go run . -cli -input .\domains.txt -family all -use-cache
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
  "trace": false,
  "use_cache": false
}
```

Windows build notes:

- Fyne on Windows needs `CGO_ENABLED=1`
- install MSYS2 UCRT64 gcc and add `C:\msys64\ucrt64\bin` to `PATH`
- the packaging script checks for `gcc` before building

Packaging:

```powershell
.\package.ps1
```

Notes:

- Running with no arguments launches the GUI.
- If `config.json` does not exist, the program creates one with the default CN domains.
- Trace probing is off by default to keep scans responsive.
- For Genshin hosts updates, the best result for `autopatchcn.yuanshen.com` is also written to `autopatchhk.yuanshen.com` and `genshinimpact.mihoyo.com`.
