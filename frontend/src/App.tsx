import {useEffect, useState} from 'react';
import './App.css';
import {
  Bootstrap,
  ExportLatestCSV,
  LookupGeoIP,
  RouteTunnel,
  RouteTunnelBatch,
  SaveSettings,
  Scan,
  StopTunnel,
  TraceCandidate,
  WriteHosts,
  WriteHostsBatch,
} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {BrowserOpenURL, EventsOff, EventsOn} from '../wailsjs/runtime/runtime';

type GameView = {
  id: string;
  key: string;
  name: string;
  enabled: boolean;
  domainCount: number;
  domains: string[];
  preferredProvider: string;
  providerOptions: string[];
};

type BootstrapData = {
  configPath: string;
  games: GameView[];
  resolvers: string[];
  family: string;
  geoip: GeoIPSettings;
  tunnelPort: number;
  tunnelActive: boolean;
  tunnelRuleCount: number;
  logFile: string;
  cacheFile: string;
  hasResults: boolean;
};

type DomainSummary = {
  domain: string;
  candidateCount: number;
  bestIP: string;
  bestLatency: string;
};

type CandidateView = {
  domain: string;
  ipAddress: string;
  resolver: string;
  resolvers: string[];
  latency: string;
  family: string;
  cname: string;
  note: string;
  aliases: string[];
  connectOk: boolean;
  hopCount: number;
  traceStatus: string;
  traceReached: boolean;
};

type ScanResponse = {
  usedCache: boolean;
  domains: DomainSummary[];
  candidates: CandidateView[];
};

type ProgressPayload = {
  total: number;
  completed: number;
  activeCount: number;
  activeSummary: string;
  lastCompleted: string;
  elapsed: string;
  percent: number;
  final: boolean;
};

type TraceResponse = {
  domain: string;
  ipAddress: string;
  family: string;
  status: string;
  hopCount: number;
  reached: boolean;
  note: string;
  hops: TraceHop[];
  rawOutput: string;
};

type GeoIPSettings = {
  primaryProvider: string;
  fallbackProvider: string;
  ipinfoToken: string;
  cacheFile: string;
  mmdbCityPath: string;
  mmdbAsnPath: string;
};

type GeoIPResponse = {
  domain: string;
  ipAddress: string;
  geo: string;
  network: string;
  provider: string;
  cached: boolean;
  note: string;
};

type TraceHop = {
  hop: number;
  ipAddress: string;
  hostname: string;
  geo: string;
  network: string;
  rtt: string;
  status: string;
  rawLine: string;
};

type TunnelResponse = {
  port: number;
  listener: string;
  path: string;
  hostnames: string[];
  entries: number;
  ruleCount: number;
  active: boolean;
  note: string;
};

type PageMode = 'domains' | 'candidates' | 'details';

const defaultBootstrap: BootstrapData = {
  configPath: 'config.json',
  games: [],
  resolvers: [],
  family: '6',
  geoip: {
    primaryProvider: 'ipwho.is',
    fallbackProvider: 'ipinfo_lite',
    ipinfoToken: '',
    cacheFile: 'geoip_cache.json',
    mmdbCityPath: '',
    mmdbAsnPath: '',
  },
  tunnelPort: 0,
  tunnelActive: false,
  tunnelRuleCount: 0,
  logFile: 'latest_scan.log',
  cacheFile: 'scan_cache.json',
  hasResults: false,
};

function App() {
  const [bootstrap, setBootstrap] = useState<BootstrapData>(defaultBootstrap);
  const [draft, setDraft] = useState<BootstrapData>(defaultBootstrap);
  const [scanData, setScanData] = useState<ScanResponse>({usedCache: false, domains: [], candidates: []});
  const [page, setPage] = useState<PageMode>('domains');
  const [selectedDomain, setSelectedDomain] = useState<string>('');
  const [selectedCandidate, setSelectedCandidate] = useState<CandidateView | null>(null);
  const [checkedCandidates, setCheckedCandidates] = useState<string[]>([]);
  const [progress, setProgress] = useState<ProgressPayload | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [status, setStatus] = useState<string>('Ready');
  const [busy, setBusy] = useState<boolean>(false);
  const [settingsOpen, setSettingsOpen] = useState<boolean>(false);
  const [gameSettingsKey, setGameSettingsKey] = useState<string>('');
  const [geoIPSettingsOpen, setGeoIPSettingsOpen] = useState<boolean>(false);
  const [geoIPResult, setGeoIPResult] = useState<GeoIPResponse | null>(null);
  const [geoIPBusy, setGeoIPBusy] = useState<boolean>(false);
  const [traceResult, setTraceResult] = useState<TraceResponse | null>(null);

  useEffect(() => {
    void loadBootstrap();
  }, []);

  useEffect(() => {
    const onReset = () => {
      setLogs([]);
      setProgress(null);
      setTraceResult(null);
    };

    const onStatus = (payload: { running?: boolean; message?: string; usedCache?: boolean }) => {
      setBusy(Boolean(payload?.running));
      setStatus(payload?.message || 'Ready');
    };

    const onProgress = (payload: ProgressPayload) => {
      setProgress(payload);
    };

    const onLog = (payload: { time?: string; message?: string }) => {
      const line = `[${payload?.time || '--:--:--'}] ${payload?.message || ''}`.trim();
      setLogs(current => {
        const next = [...current, line];
        return next.slice(-120);
      });
    };

    const onTraceDone = (payload: TraceResponse) => {
      setTraceResult(payload);
    };

    EventsOn('scan:reset', onReset);
    EventsOn('scan:status', onStatus);
    EventsOn('scan:progress', onProgress);
    EventsOn('scan:log', onLog);
    EventsOn('trace:done', onTraceDone);

    return () => {
      EventsOff('scan:reset');
      EventsOff('scan:status');
      EventsOff('scan:progress');
      EventsOff('scan:log');
      EventsOff('trace:done');
    };
  }, []);

  useEffect(() => {
    if (page !== 'details' || !selectedCandidate) {
      setGeoIPResult(null);
      setGeoIPBusy(false);
      return;
    }

    let cancelled = false;
    setGeoIPBusy(true);

    void LookupGeoIP(main.candidateRequest.createFrom({
      domain: selectedCandidate.domain,
      ipAddress: selectedCandidate.ipAddress,
    }))
      .then(result => {
        if (!cancelled) {
          setGeoIPResult(result as GeoIPResponse);
        }
      })
      .catch(error => {
        if (!cancelled) {
          setGeoIPResult({
            domain: selectedCandidate.domain,
            ipAddress: selectedCandidate.ipAddress,
            geo: '-',
            network: '-',
            provider: '-',
            cached: false,
            note: formatError(error),
          });
        }
      })
      .finally(() => {
        if (!cancelled) {
          setGeoIPBusy(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [page, selectedCandidate]);

  async function loadBootstrap() {
    setBusy(true);
    try {
      const next = normalizeBootstrap(await Bootstrap() as unknown as BootstrapData);
      setBootstrap(next);
      setDraft(next);
      setStatus(`Loaded ${next.games.length} games from ${next.configPath}`);
    } catch (error) {
      setStatus(formatError(error));
    } finally {
      setBusy(false);
    }
  }

  function closeSettings() {
    setGameSettingsKey('');
    setGeoIPSettingsOpen(false);
    setSettingsOpen(false);
  }

  async function runScan() {
    setBusy(true);
    setStatus('Starting scan...');
    try {
      const next = await Scan() as ScanResponse;
      setScanData(next);
      setPage('domains');
      setSelectedDomain('');
      setSelectedCandidate(null);
      setCheckedCandidates([]);
      setTraceResult(null);
      setStatus(next.usedCache ? 'Loaded scan results from cache' : `Live scan completed with ${next.candidates.length} candidates`);
    } catch (error) {
      setStatus(formatError(error));
    } finally {
      setBusy(false);
    }
  }

  async function saveSettings() {
    setBusy(true);
    try {
      const next = normalizeBootstrap(await SaveSettings(main.settingsPayload.createFrom({
        games: draft.games.map(game => ({
          key: game.key,
          enabled: game.enabled,
          preferredProvider: game.preferredProvider,
        })),
        resolvers: draft.resolvers,
        family: draft.family,
        geoip: draft.geoip,
        tunnelPort: draft.tunnelPort,
      })) as unknown as BootstrapData);
      setBootstrap(next);
      setDraft(next);
      closeSettings();
      setStatus(`Saved settings to ${next.configPath}`);
    } catch (error) {
      setStatus(formatError(error));
    } finally {
      setBusy(false);
    }
  }

  async function exportLatestCsv() {
    setBusy(true);
    try {
      const path = await ExportLatestCSV();
      setStatus(`CSV saved to ${path}`);
    } catch (error) {
      setStatus(formatError(error));
    } finally {
      setBusy(false);
    }
  }

  async function runTrace() {
    if (!selectedCandidate) {
      return;
    }
    setBusy(true);
    setStatus(`Tracing ${selectedCandidate.ipAddress}...`);
    try {
      const result = await TraceCandidate(main.candidateRequest.createFrom({
        domain: selectedCandidate.domain,
        ipAddress: selectedCandidate.ipAddress,
      })) as TraceResponse;
      setTraceResult(result);
      setStatus(`Trace finished: ${result.status}`);
    } catch (error) {
      setStatus(formatError(error));
    } finally {
      setBusy(false);
    }
  }

  async function writeHosts() {
    if (!selectedCandidate) {
      return;
    }
    setBusy(true);
    try {
      const result = await WriteHosts(main.candidateRequest.createFrom({
        domain: selectedCandidate.domain,
        ipAddress: selectedCandidate.ipAddress,
      })) as { path: string; hostnames: string[] };
      setStatus(`Hosts updated: ${result.path}`);
    } catch (error) {
      setStatus(formatError(error));
    } finally {
      setBusy(false);
    }
  }

  async function writeCheckedHosts() {
    if (checkedCandidates.length === 0) {
      return;
    }

    const requests = scanData.candidates
      .filter(candidate => checkedCandidates.includes(candidateKey(candidate)))
      .map(candidate => ({
        domain: candidate.domain,
        ipAddress: candidate.ipAddress,
      }));

    setBusy(true);
    try {
      const result = await WriteHostsBatch(requests.map(request => main.candidateRequest.createFrom(request)));
      setStatus(`Hosts updated: ${result.path} (${result.entries} selected)`);
    } catch (error) {
      setStatus(formatError(error));
    } finally {
      setBusy(false);
    }
  }

  async function routeSelectedTunnel(candidate?: CandidateView) {
    const target = candidate ?? selectedCandidate;
    if (!target) {
      return;
    }

    setBusy(true);
    try {
      const result = await RouteTunnel(main.candidateRequest.createFrom({
        domain: target.domain,
        ipAddress: target.ipAddress,
      })) as TunnelResponse;
      setBootstrap(current => ({...current, tunnelActive: result.active, tunnelRuleCount: result.ruleCount, tunnelPort: result.port || current.tunnelPort}));
      setDraft(current => ({...current, tunnelActive: result.active, tunnelRuleCount: result.ruleCount, tunnelPort: result.port || current.tunnelPort}));
      setStatus(`Tunnel active on ${result.listener} with ${result.ruleCount} hostnames`);
    } catch (error) {
      setStatus(formatError(error));
    } finally {
      setBusy(false);
    }
  }

  async function routeCheckedTunnel() {
    if (checkedCandidates.length === 0) {
      return;
    }

    const requests = scanData.candidates
      .filter(candidate => checkedCandidates.includes(candidateKey(candidate)))
      .map(candidate => ({
        domain: candidate.domain,
        ipAddress: candidate.ipAddress,
      }));

    setBusy(true);
    try {
      const result = await RouteTunnelBatch(requests.map(request => main.candidateRequest.createFrom(request))) as TunnelResponse;
      setBootstrap(current => ({...current, tunnelActive: result.active, tunnelRuleCount: result.ruleCount, tunnelPort: result.port || current.tunnelPort}));
      setDraft(current => ({...current, tunnelActive: result.active, tunnelRuleCount: result.ruleCount, tunnelPort: result.port || current.tunnelPort}));
      setStatus(`Tunnel active on ${result.listener} with ${result.ruleCount} hostnames`);
    } catch (error) {
      setStatus(formatError(error));
    } finally {
      setBusy(false);
    }
  }

  async function stopTunnelRouting() {
    setBusy(true);
    try {
      const result = await StopTunnel() as TunnelResponse;
      setBootstrap(current => ({...current, tunnelActive: false, tunnelRuleCount: 0}));
      setDraft(current => ({...current, tunnelActive: false, tunnelRuleCount: 0}));
      setStatus(result.note || 'Tunnel disabled');
    } catch (error) {
      setStatus(formatError(error));
    } finally {
      setBusy(false);
    }
  }

  function openDomain(domain: string) {
    setSelectedDomain(domain);
    setSelectedCandidate(null);
    setTraceResult(null);
    setPage('candidates');
  }

  function openCandidate(candidate: CandidateView) {
    setSelectedCandidate(candidate);
    setTraceResult(null);
    setPage('details');
  }

  function updateResolverText(text: string) {
    setDraft(current => ({
      ...current,
      resolvers: splitResolverText(text),
    }));
  }

  const selectedGameSettings = draft.games.find(game => game.key === gameSettingsKey) || null;
  const currentCandidates = scanData.candidates.filter(candidate => candidate.domain === selectedDomain);
  const selectedCandidates = scanData.candidates.filter(candidate => checkedCandidates.includes(candidateKey(candidate)));
  const checkedCount = selectedCandidates.length;
  const currentDomainCheckedCount = currentCandidates.filter(candidate => checkedCandidates.includes(candidateKey(candidate))).length;
  const selectedCountByDomain = selectedCandidates.reduce<Record<string, number>>((acc, candidate) => {
    acc[candidate.domain] = (acc[candidate.domain] || 0) + 1;
    return acc;
  }, {});

  function toggleCandidate(candidate: CandidateView, enabled: boolean) {
    const key = candidateKey(candidate);
    setCheckedCandidates(current => {
      if (enabled) {
        if (current.includes(key)) {
          return current;
        }
        return [...current, key];
      }
      return current.filter(item => item !== key);
    });
  }

  function clearSelectedCandidates() {
    setCheckedCandidates([]);
  }

  return (
    <div className="app-shell">
      <header className="top-shell">
        <div className="title-block">
          <div className="title-mark" aria-hidden="true">
            <span/>
            <span/>
            <span/>
          </div>
          <div>
            <h1>CDN Selector</h1>
            <p className="subcopy">{status}</p>
          </div>
        </div>

        <div className="toolbar">
          <button className="tool-button ghost" onClick={() => setSettingsOpen(true)}>Settings</button>
          <button className="tool-button primary" onClick={runScan} disabled={busy}>Start Scan</button>
          <button className="tool-button ghost" onClick={exportLatestCsv} disabled={busy || scanData.candidates.length === 0}>Save CSV</button>
        </div>

        <div className="telemetry">
          <div className="progress-meta">
            <span>{progress ? `${progress.completed}/${progress.total} domains` : 'Idle'}</span>
            <span>{progress ? progress.elapsed : '00:00'}</span>
            <span>{bootstrap.tunnelActive ? `Tunnel ${bootstrap.tunnelPort || 'auto'} · ${bootstrap.tunnelRuleCount} routes` : 'Tunnel off'}</span>
          </div>
          <div className="progress-track">
            <div className="progress-fill" style={{width: `${Math.round((progress?.percent || 0) * 100)}%`}}/>
          </div>
          <div className="log-strip">
            {(logs.length > 0 ? logs : ['No log output yet. Start a scan to stream activity here.']).map((line, index) => (
              <div className="log-line" key={`${line}-${index}`}>{line}</div>
            ))}
          </div>
        </div>
      </header>

      <main className="page-shell">
        <div className="breadcrumb-row">
          <button className={`crumb ${page === 'domains' ? 'active' : ''}`} onClick={() => setPage('domains')}>Domains</button>
          <button
            className={`crumb ${page === 'candidates' ? 'active' : ''}`}
            onClick={() => selectedDomain && setPage('candidates')}
            disabled={!selectedDomain}
          >
            {selectedDomain || 'Candidates'}
          </button>
          <button
            className={`crumb ${page === 'details' ? 'active' : ''}`}
            onClick={() => selectedCandidate && setPage('details')}
            disabled={!selectedCandidate}
          >
            {selectedCandidate?.ipAddress || 'Details'}
          </button>
        </div>

        {page === 'domains' && (
          <section className="panel">
            <div className="panel-heading">
              <div>
                <h2>Domains</h2>
              </div>
              <div className="panel-actions">
                <span className="panel-note">{scanData.domains.length} rows</span>
                <span className="panel-note">{checkedCount} selected</span>
                <button className="tool-button ghost compact" onClick={routeCheckedTunnel} disabled={busy || checkedCount === 0}>
                  Route Selected via Tunnel
                </button>
                <button className="tool-button ghost compact" onClick={writeCheckedHosts} disabled={busy || checkedCount === 0}>
                  Write Selected to Hosts
                </button>
                <button className="tool-button ghost compact" onClick={clearSelectedCandidates} disabled={busy || checkedCount === 0}>
                  Clear Selection
                </button>
                <button className="tool-button ghost compact" onClick={stopTunnelRouting} disabled={busy || !bootstrap.tunnelActive}>
                  Stop Tunnel
                </button>
              </div>
            </div>
            <div className="table-shell">
              <table className="data-table">
                <thead>
                <tr>
                  <th>Domain</th>
                  <th>Candidate Count</th>
                  <th>Selected</th>
                  <th>Best IP</th>
                  <th>Latency</th>
                </tr>
                </thead>
                <tbody>
                {scanData.domains.map(domain => (
                  <tr key={domain.domain} onClick={() => openDomain(domain.domain)}>
                    <td className="mono">{domain.domain}</td>
                    <td>{domain.candidateCount}</td>
                    <td>{selectedCountByDomain[domain.domain] || 0}</td>
                    <td className="mono wrap">{domain.bestIP || '-'}</td>
                    <td>{domain.bestLatency}</td>
                  </tr>
                ))}
                </tbody>
              </table>
              {scanData.domains.length === 0 && <EmptyState label="Run a scan to populate domain candidates."/>}
            </div>
          </section>
        )}

        {page === 'candidates' && (
          <section className="panel">
            <div className="panel-heading">
              <div>
                <h2>{selectedDomain}</h2>
              </div>
              <div className="panel-actions">
                <span className="panel-note">{currentDomainCheckedCount} in this domain</span>
                <span className="panel-note">{checkedCount} total selected</span>
                <button className="tool-button ghost compact" onClick={routeCheckedTunnel} disabled={busy || checkedCount === 0}>
                  Route Selected via Tunnel
                </button>
                <button className="tool-button ghost compact" onClick={writeCheckedHosts} disabled={busy || checkedCount === 0}>
                  Write Selected to Hosts
                </button>
                <button className="tool-button ghost compact" onClick={clearSelectedCandidates} disabled={busy || checkedCount === 0}>
                  Clear Selection
                </button>
                <button className="tool-button ghost compact" onClick={stopTunnelRouting} disabled={busy || !bootstrap.tunnelActive}>
                  Stop Tunnel
                </button>
                <button className="tool-button ghost compact" onClick={() => setPage('domains')}>Back to Domains</button>
              </div>
            </div>
            <div className="table-shell">
              <table className="data-table candidates-table">
                <thead>
                <tr>
                  <th className="checkbox-col">Use</th>
                  <th>Domain</th>
                  <th>IP Address</th>
                  <th>Resolver</th>
                  <th>Latency</th>
                </tr>
                </thead>
                <tbody>
                {currentCandidates.map(candidate => (
                  <tr key={`${candidate.domain}-${candidate.ipAddress}`} onClick={() => openCandidate(candidate)}>
                    <td className="checkbox-cell" onClick={event => event.stopPropagation()}>
                      <input
                        type="checkbox"
                        checked={checkedCandidates.includes(candidateKey(candidate))}
                        onChange={event => toggleCandidate(candidate, event.target.checked)}
                      />
                    </td>
                    <td className="mono wrap">{candidate.domain}</td>
                    <td className="mono wrap">{candidate.ipAddress || '-'}</td>
                    <td className="mono multiline">{candidate.resolvers.join('\n') || '-'}</td>
                    <td>{candidate.latency}</td>
                  </tr>
                ))}
                </tbody>
              </table>
              {currentCandidates.length === 0 && <EmptyState label="No candidates for this domain yet."/>}
            </div>
          </section>
        )}

        {page === 'details' && selectedCandidate && (
          <section className="panel details-panel">
            <div className="panel-heading">
              <div>
                <h2>{selectedCandidate.ipAddress}</h2>
              </div>
              <button className="tool-button ghost compact" onClick={() => setPage('candidates')}>Back to Candidates</button>
            </div>

            <div className="details-scroll">
              <div className="detail-grid">
                <DetailCard label="Domain" value={selectedCandidate.domain}/>
                <DetailCard label="IP Address" value={selectedCandidate.ipAddress}/>
                <DetailCard label="Latency" value={selectedCandidate.latency}/>
                <DetailCard label="Address Family" value={selectedCandidate.family === '4' ? 'IPv4' : 'IPv6'}/>
                <DetailCard label="GeoIP" value={geoIPBusy ? 'Loading...' : geoIPResult?.geo || '-'}/>
                <DetailCard label="Network" value={geoIPBusy ? 'Loading...' : geoIPResult?.network || '-'}/>
                <DetailCard
                  label="GeoIP Source"
                  value={geoIPBusy ? 'Loading...' : formatGeoIPSource(geoIPResult)}
                />
                <DetailCard label="Resolver" value={selectedCandidate.resolvers.join('\n')}/>
                <DetailCard label="CNAME" value={selectedCandidate.cname}/>
                <DetailCard label="Aliases" value={selectedCandidate.aliases.join('\n')}/>
                <DetailCard label="Note" value={selectedCandidate.note}/>
              </div>

              <div className="detail-actions">
                <button className="tool-button primary" onClick={runTrace} disabled={busy}>Run Trace</button>
                <button className="tool-button ghost" onClick={() => routeSelectedTunnel(selectedCandidate)} disabled={busy}>Route via Tunnel</button>
                <button className="tool-button ghost" onClick={writeHosts} disabled={busy}>Write Hosts</button>
                <button className="tool-button ghost" onClick={stopTunnelRouting} disabled={busy || !bootstrap.tunnelActive}>Stop Tunnel</button>
              </div>

              <div className="trace-panel">
                <div>
                  <h3>Trace Details</h3>
                </div>
                {traceResult ? (
                  <>
                    <div className="trace-result">
                      <div><strong>Status:</strong> {traceResult.status}</div>
                      <div><strong>Hop Count:</strong> {traceResult.hopCount || '-'}</div>
                      <div><strong>Reached:</strong> {traceResult.reached ? 'Yes' : 'No'}</div>
                      <div><strong>Note:</strong> {traceResult.note || '-'}</div>
                    </div>
                    <div className="trace-table-shell">
                      <table className="data-table trace-table">
                        <thead>
                        <tr>
                          <th>Hop</th>
                          <th>IP Address</th>
                          <th>Hostname</th>
                          <th>GeoIP</th>
                          <th>Network</th>
                          <th>RTT / Status</th>
                        </tr>
                        </thead>
                        <tbody>
                        {traceResult.hops.map(hop => (
                          <tr key={`${hop.hop}-${hop.ipAddress}-${hop.rawLine}`}>
                            <td>{hop.hop}</td>
                            <td className="mono wrap">{hop.ipAddress || '-'}</td>
                            <td className="wrap">{hop.hostname || '-'}</td>
                            <td className="wrap">{hop.geo || '-'}</td>
                            <td className="wrap">{hop.network || '-'}</td>
                            <td className="wrap">{hop.rtt || hop.status || '-'}</td>
                          </tr>
                        ))}
                        </tbody>
                      </table>
                    </div>
                    <div className="raw-trace-block">
                      <strong>Raw Trace</strong>
                      <pre>{traceResult.rawOutput || '-'}</pre>
                    </div>
                  </>
                ) : (
                  <p className="trace-placeholder">Run trace here when you want hop-by-hop route details. Normal scans do not trace in the background.</p>
                )}
              </div>
            </div>
          </section>
        )}
      </main>

      {settingsOpen && (
        <div className="settings-overlay" onClick={closeSettings}>
          <aside className="settings-drawer" onClick={event => event.stopPropagation()}>
            <div className="drawer-head">
              <div>
                <h2>Targets and Network</h2>
              </div>
              <button className="tool-button ghost compact" onClick={closeSettings}>Close</button>
            </div>

            <div className="drawer-body">
              <section className="drawer-section">
                <h3>Games</h3>
                <div className="game-list">
                  {draft.games.map(game => (
                    <label className="toggle-row" key={game.key}>
                      <input
                        type="checkbox"
                        checked={game.enabled}
                        onChange={event => {
                          setDraft(current => ({
                            ...current,
                            games: current.games.map(item => item.key === game.key ? {...item, enabled: event.target.checked} : item),
                          }));
                        }}
                      />
                      <span>
                        <strong>{game.name}</strong>
                        <small>{game.domains.join(', ')}</small>
                        {game.providerOptions.length > 0 && (
                          <small>Provider: {providerLabel(game.preferredProvider)}</small>
                        )}
                      </span>
                      {game.providerOptions.length > 0 && (
                        <button
                          type="button"
                          className="detail-menu-button"
                          onClick={event => {
                            event.preventDefault();
                            event.stopPropagation();
                            setGameSettingsKey(game.key);
                          }}
                          aria-label={`Open ${game.name} settings`}
                        >
                          ...
                        </button>
                      )}
                    </label>
                  ))}
                </div>
                <p className="hint">Games with multiple CDN providers can be narrowed to one provider here. Only the managed download group is affected.</p>
              </section>

              <section className="drawer-section">
                <h3>Address Family</h3>
                <div className="segmented">
                  {['4', '6', 'all'].map(family => (
                    <button
                      key={family}
                      className={`segment ${draft.family === family ? 'active' : ''}`}
                      onClick={() => setDraft(current => ({...current, family}))}
                    >
                      {family === '4' ? 'IPv4' : family === '6' ? 'IPv6' : 'Dual Stack'}
                    </button>
                  ))}
                </div>
              </section>

              <section className="drawer-section">
                <h3>DNS Resolvers</h3>
                <textarea
                  className="resolver-input"
                  value={draft.resolvers.join('\n')}
                  onChange={event => updateResolverText(event.target.value)}
                  spellCheck={false}
                />
                <p className="hint">One resolver per line. Active adapter DNS servers are always merged automatically.</p>
              </section>

              <section className="drawer-section">
                <div className="section-header">
                  <div>
                    <h3>GeoIP Lookup</h3>
                    <p className="hint">Only used in `Details` and `Trace`. Normal scans do not call GeoIP APIs.</p>
                  </div>
                  <button className="tool-button ghost compact" onClick={() => setGeoIPSettingsOpen(true)}>Configure</button>
                </div>
                <div className="summary-list">
                  <div><strong>Primary:</strong> {draft.geoip.primaryProvider || '-'}</div>
                  <div><strong>Fallback:</strong> {draft.geoip.fallbackProvider || 'none'}</div>
                  <div><strong>Cache:</strong> {draft.geoip.cacheFile || 'geoip_cache.json'}</div>
                  <div><strong>IPinfo token:</strong> {draft.geoip.ipinfoToken ? 'configured' : 'not set'}</div>
                  <div><strong>MMDB city:</strong> {draft.geoip.mmdbCityPath || 'not set'}</div>
                  <div><strong>MMDB ASN:</strong> {draft.geoip.mmdbAsnPath || 'not set'}</div>
                </div>
              </section>

              <section className="drawer-section">
                <h3>Local Tunnel</h3>
                <label className="toggle-row compact-toggle">
                  <span>
                    <strong>Preferred listen port</strong>
                    <small>Use `0` for a random local port. Windows loopback portproxy still catches `443`.</small>
                  </span>
                  <input
                    type="number"
                    min={0}
                    max={65535}
                    value={draft.tunnelPort}
                    onChange={event => setDraft(current => ({
                      ...current,
                      tunnelPort: Math.max(0, Number.parseInt(event.target.value || '0', 10) || 0),
                    }))}
                  />
                </label>
                <p className="hint">
                  Current status: {bootstrap.tunnelActive ? `active with ${bootstrap.tunnelRuleCount} routed hostnames` : 'inactive'}.
                </p>
              </section>
            </div>

            <div className="drawer-foot">
              <button className="tool-button ghost" onClick={() => setDraft(normalizeBootstrap(bootstrap))}>Reset</button>
              <button className="tool-button primary" onClick={saveSettings} disabled={busy}>Save Settings</button>
            </div>
          </aside>

          {selectedGameSettings && (
            <div className="settings-suboverlay" onClick={() => setGameSettingsKey('')}>
              <section className="settings-subdialog game-settings-dialog" onClick={event => event.stopPropagation()}>
                <div className="drawer-head">
                  <div>
                    <h2>{selectedGameSettings.name}</h2>
                  </div>
                  <button className="tool-button ghost compact" onClick={() => setGameSettingsKey('')}>Close</button>
                </div>

                <div className="drawer-body">
                  <section className="drawer-section">
                    <h3>Managed Download Domains</h3>
                    <div className="summary-list">
                      {selectedGameSettings.domains.map(domain => (
                        <div key={domain} className="mono">{domain}</div>
                      ))}
                    </div>
                  </section>

                  <section className="drawer-section">
                    <h3>Download Provider</h3>
                    <select
                      className="select-input"
                      value={selectedGameSettings.preferredProvider || 'auto'}
                      onChange={event => {
                        const preferredProvider = event.target.value;
                        setDraft(current => ({
                          ...current,
                          games: current.games.map(item => item.key === selectedGameSettings.key ? {...item, preferredProvider} : item),
                        }));
                      }}
                    >
                      <option value="auto">Auto</option>
                      {selectedGameSettings.providerOptions.map(provider => (
                        <option key={provider} value={provider}>{providerLabel(provider)}</option>
                      ))}
                    </select>
                    <p className="hint">
                      `Auto` keeps all managed download providers available. Choosing one provider applies strict mode and blocks the other managed download providers for this game when you write hosts or enable tunnel routing.
                    </p>
                  </section>
                </div>
              </section>
            </div>
          )}

          {geoIPSettingsOpen && (
            <div className="settings-suboverlay" onClick={() => setGeoIPSettingsOpen(false)}>
              <section className="settings-subdialog" onClick={event => event.stopPropagation()}>
                <div className="drawer-head">
                  <div>
                    <h2>GeoIP Lookup</h2>
                  </div>
                  <button className="tool-button ghost compact" onClick={() => setGeoIPSettingsOpen(false)}>Close</button>
                </div>

                <div className="drawer-body">
                  <section className="drawer-section">
                    <h3>Primary Provider</h3>
                    <select
                      className="select-input"
                      value={draft.geoip.primaryProvider || 'ipwho.is'}
                      onChange={event => setDraft(current => ({
                        ...current,
                        geoip: {...current.geoip, primaryProvider: event.target.value},
                      }))}
                    >
                      <option value="ipwho.is">ipwho.is</option>
                      <option value="ipinfo_lite">IPinfo Lite</option>
                      <option value="mmdb">Local MMDB</option>
                    </select>
                  </section>

                  <section className="drawer-section">
                    <h3>Fallback Provider</h3>
                    <select
                      className="select-input"
                      value={draft.geoip.fallbackProvider || 'none'}
                      onChange={event => setDraft(current => ({
                        ...current,
                        geoip: {...current.geoip, fallbackProvider: event.target.value},
                      }))}
                    >
                      <option value="none">None</option>
                      <option value="ipwho.is">ipwho.is</option>
                      <option value="ipinfo_lite">IPinfo Lite</option>
                      <option value="mmdb">Local MMDB</option>
                    </select>
                    <p className="hint">`ipwho.is` works without a token. `IPinfo Lite` needs a token if used as fallback.</p>
                  </section>

                  <section className="drawer-section">
                    <h3>IPinfo Lite Token</h3>
                    <input
                      className="text-input"
                      type="password"
                      value={draft.geoip.ipinfoToken}
                      onChange={event => setDraft(current => ({
                        ...current,
                        geoip: {...current.geoip, ipinfoToken: event.target.value},
                      }))}
                      spellCheck={false}
                      autoComplete="off"
                    />
                  </section>

                  <section className="drawer-section">
                    <h3>Cache File</h3>
                    <input
                      className="text-input mono-input"
                      type="text"
                      value={draft.geoip.cacheFile}
                      onChange={event => setDraft(current => ({
                        ...current,
                        geoip: {...current.geoip, cacheFile: event.target.value},
                      }))}
                      spellCheck={false}
                    />
                    <p className="hint">Default is `geoip_cache.json` in the app working directory.</p>
                  </section>

                  <section className="drawer-section">
                    <h3>Local MMDB Files</h3>
                    <input
                      className="text-input mono-input"
                      type="text"
                      placeholder="City MMDB path"
                      value={draft.geoip.mmdbCityPath}
                      onChange={event => setDraft(current => ({
                        ...current,
                        geoip: {...current.geoip, mmdbCityPath: event.target.value},
                      }))}
                      spellCheck={false}
                    />
                    <input
                      className="text-input mono-input"
                      type="text"
                      placeholder="ASN MMDB path"
                      value={draft.geoip.mmdbAsnPath}
                      onChange={event => setDraft(current => ({
                        ...current,
                        geoip: {...current.geoip, mmdbAsnPath: event.target.value},
                      }))}
                      spellCheck={false}
                    />
                    <p className="hint">Use local City and ASN MMDB files when `Local MMDB` is selected as primary or fallback.</p>
                  </section>

                  <section className="drawer-section">
                    <h3>MMDB Sources</h3>
                    <div className="download-grid">
                      <div className="download-card">
                        <strong>DB-IP Lite</strong>
                        <p className="hint">Official lite MMDB downloads. No account is required.</p>
                        <button className="tool-button ghost compact" onClick={() => BrowserOpenURL('https://db-ip.com/db/lite.php')}>Open DB-IP Lite</button>
                      </div>
                      <div className="download-card">
                        <strong>MaxMind GeoLite2</strong>
                        <p className="hint">Official MMDB source. A MaxMind account and license key are required.</p>
                        <button className="tool-button ghost compact" onClick={() => BrowserOpenURL('https://support.maxmind.com/hc/en-us/articles/4408216129947-Download-and-Update-Databases')}>Open MaxMind Guide</button>
                      </div>
                    </div>
                  </section>
                </div>
              </section>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function EmptyState({label}: { label: string }) {
  return (
    <div className="empty-state">
      <div className="empty-mark"/>
      <p>{label}</p>
    </div>
  );
}

function DetailCard({label, value}: { label: string; value: string }) {
  return (
    <div className="detail-card">
      <p>{label}</p>
      <div className="detail-value">{value || '-'}</div>
    </div>
  );
}

function normalizeBootstrap(input: BootstrapData): BootstrapData {
  return {
    ...defaultBootstrap,
    ...input,
    games: Array.isArray(input.games) ? input.games.map(normalizeGameView) : [],
    resolvers: Array.isArray(input.resolvers) ? input.resolvers.filter(Boolean) : [],
    geoip: {
      ...defaultBootstrap.geoip,
      ...(input.geoip || {}),
    },
  };
}

function normalizeGameView(input: Partial<GameView>): GameView {
  return {
    id: input.id || '',
    key: input.key || '',
    name: input.name || '',
    enabled: Boolean(input.enabled),
    domainCount: Number(input.domainCount || 0),
    domains: Array.isArray(input.domains) ? input.domains.filter(Boolean) : [],
    preferredProvider: input.preferredProvider || 'auto',
    providerOptions: Array.isArray(input.providerOptions) ? input.providerOptions.filter(Boolean) : [],
  };
}

function splitResolverText(input: string): string[] {
  return input
    .split(/[\n\r,;\t ]+/)
    .map(value => value.trim())
    .filter(Boolean);
}

function candidateKey(candidate: CandidateView): string {
  return `${candidate.domain}|${candidate.ipAddress}`;
}

function formatGeoIPSource(result: GeoIPResponse | null): string {
  if (!result) {
    return '-';
  }

  const provider = result.provider || '-';
  const suffix = result.cached ? ' (cache)' : '';
  if (result.note && result.note !== '-') {
    return `${provider}${suffix}\n${result.note}`;
  }
  return `${provider}${suffix}`;
}

function providerLabel(value: string): string {
  switch ((value || '').toLowerCase()) {
    case '':
    case 'auto':
      return 'Auto';
    case 'aliyun':
      return 'Aliyun';
    case 'huoshan':
      return 'Huoshan';
    case 'qcloud':
      return 'QCloud';
    default:
      return value;
  }
}

function formatError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

export default App;
