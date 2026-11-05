package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var appVersion = "v1.0.0"

type Config struct {
	FriendlyName     string            `json:"friendly_name"`
	TunerCount       int               `json:"tuner_count"`
	M3UURLs          string            `json:"m3u_urls"`
	CustomChannels   string            `json:"custom_channels"`
	XMLTVURLs        string            `json:"xmltv_urls"`
	XMLTVURL         string            `json:"xmltv_url,omitempty"`
	FilterKeywords   string            `json:"filter_keywords"`
	EnabledChannels  map[string]bool   `json:"enabled_channels"`
	DisabledChannels map[string]bool   `json:"disabled_channels"`
	CustomNumbers    map[string]string `json:"custom_numbers"`
}

type Channel struct {
	Key         string `json:"Key"`
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	Logo        string `json:"Logo,omitempty"`
	TvgID       string `json:"TvgID,omitempty"`
	Group       string `json:"Group,omitempty"`
	Enabled     bool   `json:"Enabled"`
}

type ChannelStateUpdate struct {
	Key     string `json:"key"`
	URL     string `json:"url"`
	Num     string `json:"num"`
	Enabled bool   `json:"enabled"`
}

type DeviceInfo struct {
	FriendlyName    string `json:"FriendlyName"`
	ModelNumber     string `json:"ModelNumber"`
	FirmwareName    string `json:"FirmwareName"`
	FirmwareVersion string `json:"FirmwareVersion"`
	DeviceID        string `json:"DeviceID"`
	DeviceAuth      string `json:"DeviceAuth"`
	TunerCount      int    `json:"TunerCount"`
	BaseURL         string `json:"BaseURL"`
	LineupURL       string `json:"LineupURL"`
}

type LineupStatus struct {
	ScanInProgress int      `json:"ScanInProgress"`
	ScanPossible   int      `json:"ScanPossible"`
	Source         string   `json:"Source"`
	SourceList     []string `json:"SourceList"`
}

type LineupItem struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
}

var (
	configLock      sync.RWMutex
	appConfig       Config
	channels        []Channel
	configPath      string
	epgPath         string
	isRefreshingEPG bool
	epgLock         sync.Mutex
)

func makeChannelKey(name, streamURL string) string {
	return strings.TrimSpace(name) + "|" + strings.TrimSpace(streamURL)
}

const uiTemplate = `<!DOCTYPE html>
<html lang="en" data-theme="dark">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>StreamTuner Dashboard</title>
  <script>
    (function() {
      const savedTheme = localStorage.getItem('st_theme') || 'dark';
      document.documentElement.setAttribute('data-theme', savedTheme);
    })();
  </script>
  <style>
    :root {
      --bg: #0e131f;
      --card: #182032;
      --input-bg: #0c101a;
      --border: #243049;
      --accent: #00d2c4;
      --accent-hover: #00b3a6;
      --btn-text: #051015;
      --text: #f0f4f8;
      --text-muted: #8e9eb5;
      --success-bg: rgba(16, 185, 129, 0.15);
      --success-border: #10b981;
    }
    html[data-theme="light"] {
      --bg: #f1f5f9;
      --card: #ffffff;
      --input-bg: #f8fafc;
      --border: #cbd5e1;
      --accent: #0d9488;
      --accent-hover: #0f766e;
      --btn-text: #ffffff;
      --text: #0f172a;
      --text-muted: #475569;
      --success-bg: rgba(13, 148, 136, 0.12);
      --success-border: #0d9488;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    body { background: var(--bg); color: var(--text); padding: 16px; display: flex; justify-content: center; transition: background 0.2s, color 0.2s; }
    .container { width: 100%; max-width: 960px; }
    header { margin-bottom: 20px; padding-bottom: 16px; border-bottom: 1px solid var(--border); }
    .header-top { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; margin-bottom: 10px; }
    .logo-group { display: flex; align-items: center; gap: 10px; }
    .logo-group h1 { font-size: 1.5rem; color: var(--accent); }
    .badge { background: var(--border); color: var(--accent); padding: 4px 10px; border-radius: 999px; font-size: 0.75rem; font-weight: 600; }
    .app-desc { color: var(--text-muted); font-size: 0.9rem; line-height: 1.5; }
    .toast { background: var(--success-bg); border: 1px solid var(--success-border); color: var(--text); padding: 12px 16px; border-radius: 10px; margin-bottom: 18px; font-size: 0.9rem; font-weight: 600; display: flex; align-items: center; justify-content: space-between; }
    .card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 20px; margin-bottom: 20px; box-shadow: 0 4px 16px rgba(0,0,0,0.12); }
    .card h2 { font-size: 1.15rem; margin-bottom: 14px; color: var(--text); }
    .form-group { margin-bottom: 14px; }
    label { display: block; margin-bottom: 6px; font-size: 0.85rem; color: var(--text-muted); font-weight: 500; }
    input[type="text"], input[type="number"], select, textarea { width: 100%; padding: 10px 12px; background: var(--input-bg); border: 1px solid var(--border); border-radius: 8px; color: var(--text); font-size: 0.95rem; }
    textarea { resize: vertical; min-height: 80px; font-family: monospace; font-size: 0.85rem; }
    input:focus, select:focus, textarea:focus { outline: none; border-color: var(--accent); }
    .btn { background: var(--accent); color: var(--btn-text); border: none; padding: 10px 18px; font-size: 0.9rem; font-weight: 700; border-radius: 8px; cursor: pointer; transition: background 0.2s; text-decoration: none; display: inline-block; }
    .btn:hover { background: var(--accent-hover); }
    .btn-secondary { background: var(--border); color: var(--text); font-weight: 600; padding: 7px 12px; font-size: 0.8rem; border-radius: 6px; text-decoration: none; cursor: pointer; border: 1px solid var(--border); }
    .btn-secondary:hover { border-color: var(--accent); }
    .stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; margin-bottom: 20px; }
    .stat-box { background: var(--card); border: 1px solid var(--border); border-radius: 10px; padding: 14px; text-align: center; }
    .stat-box .label { font-size: 0.8rem; color: var(--text-muted); font-weight: 500; }
    .stat-box .val { font-size: 1.6rem; font-weight: 700; color: var(--accent); margin-top: 4px; }
    .table-actions { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex-wrap: wrap; gap: 8px; }
    .search-bar { display: grid; grid-template-columns: 1fr auto auto; gap: 10px; margin-bottom: 12px; }
    @media (max-width: 640px) {
      .search-bar { grid-template-columns: 1fr; }
    }
    .search-meta { font-size: 0.8rem; color: var(--text-muted); margin-bottom: 10px; display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 6px; }
    .table-container { max-height: 480px; overflow-y: auto; border: 1px solid var(--border); border-radius: 8px; background: var(--input-bg); }
    .channel-table { width: 100%; border-collapse: collapse; }
    .channel-table th, .channel-table td { padding: 8px 10px; text-align: left; border-bottom: 1px solid var(--border); font-size: 0.85rem; }
    .channel-table th { position: sticky; top: 0; background: var(--card); z-index: 2; color: var(--text-muted); font-weight: 600; border-bottom: 2px solid var(--border); }
    .num-input { width: 62px; padding: 5px 6px; background: var(--card); border: 1px solid var(--border); border-radius: 4px; color: var(--accent); font-weight: 700; text-align: center; font-size: 0.85rem; }
    .num-input:focus { border-color: var(--accent); outline: none; }
    .group-pill { background: var(--border); padding: 2px 6px; border-radius: 4px; font-size: 0.75rem; color: var(--text-muted); }
    input[type="checkbox"] { width: 18px; height: 18px; accent-color: var(--accent); cursor: pointer; }
    .backup-bar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; padding-top: 12px; border-top: 1px solid var(--border); margin-top: 16px; }
    .info-list { margin: 10px 0 16px 18px; color: var(--text-muted); font-size: 0.85rem; line-height: 1.6; }
    .info-list li { margin-bottom: 6px; }
    .info-list strong { color: var(--text); }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <div class="header-top">
        <div class="logo-group">
          <h1>StreamTuner</h1>
          <span class="badge">{{.Version}}</span>
        </div>
        <button type="button" id="themeToggle" class="btn-secondary" onclick="toggleTheme()">☀️ Daytime Theme</button>
      </div>
      <p class="app-desc">
        StreamTuner is a high-stability IPTV tuner proxy, stream-repair engine, and multi-region EPG aggregator. Use it to combine live M3U playlists from around the world, automatically organize station numbers by country, prevent stream drops during commercial breaks, and feed a unified broadcast lineup into Jellyfin, Plex, or Emby.
      </p>
    </header>

    {{if .Saved}}
    <div class="toast" id="saveToast">
      <span>✓ Changes saved! {{.ActiveCount}} stations are currently active and sent to your Media Server.</span>
      <button type="button" class="btn-secondary" style="padding: 3px 8px;" onclick="document.getElementById('saveToast').style.display='none'">Dismiss</button>
    </div>
    {{end}}

    <div class="stats">
      <div class="stat-box">
        <div class="label">Virtual Tuners</div>
        <div class="val">{{.Config.TunerCount}}</div>
      </div>
      <div class="stat-box">
        <div class="label">Total Stations Found</div>
        <div class="val">{{len .Channels}}</div>
      </div>
      <div class="stat-box">
        <div class="label">Stations Sent to Media Server</div>
        <div class="val" id="liveActiveStat">{{.ActiveCount}}</div>
      </div>
    </div>

    <form id="mainForm" action="/save" method="POST" onsubmit="prepareSubmission()">
      <input type="hidden" name="action_type" id="actionType" value="apply_channels">
      <input type="hidden" name="channel_state_json" id="channelStateJSON" value="">

      <div class="card">
        <h2>Configuration &amp; Sources</h2>
        <div class="form-group">
          <label>Virtual Tuners (Simultaneous Streams Allowed)</label>
          <input type="number" name="tuner_count" value="{{.Config.TunerCount}}" min="1" max="32" required>
        </div>
        <div class="form-group">
          <label>M3U / M3U8 Playlist URLs (One URL per line — Auto-tagged by country)</label>
          <textarea name="m3u_urls" placeholder="https://example.com/au.m3u8&#10;https://example.com/us.m3u">{{.Config.M3UURLs}}</textarea>
        </div>
        <div class="form-group">
          <label>Custom Direct Channels (Format: Number, Name, Stream URL, EPG_ID)</label>
          <textarea name="custom_channels" placeholder="9, Channel 9 HD, https://9now...m3u8, mjh-channel-9-nsw">{{.Config.CustomChannels}}</textarea>
        </div>
        <div class="form-group">
          <label>XMLTV EPG URLs (One URL per line — supports .xml and .xml.gz)</label>
          <textarea name="xmltv_urls" placeholder="https://example.com/epg.xml">{{.Config.XMLTVURLs}}</textarea>
        </div>
        <div class="form-group">
          <label>Initial Playlist Keyword Filter (Optional — leave blank to control all stations with checkboxes below)</label>
          <input type="text" name="filter_keywords" value="{{.Config.FilterKeywords}}" placeholder="Leave blank to manage stations using the Search &amp; Checkboxes below">
        </div>
        
        <div style="display: flex; gap: 10px; align-items: center; flex-wrap: wrap;">
          <button type="submit" class="btn" onclick="document.getElementById('actionType').value='reload_feeds'">Save &amp; Reload Feeds</button>
        </div>

        <div class="backup-bar">
          <span style="font-size: 0.85rem; color: var(--text-muted);">Configuration Backup:</span>
          <div style="display: flex; gap: 8px; align-items: center;">
            <a href="/export-config" class="btn-secondary">Download Backup (.json)</a>
            <button type="button" class="btn-secondary" onclick="document.getElementById('importFile').click()">Restore Backup</button>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="table-actions">
          <h2>Live TV Channels ({{len .Channels}})</h2>
          <div style="display: flex; gap: 8px; flex-wrap: wrap;">
            <button type="button" class="btn-secondary" onclick="toggleVisible(true)">Select Visible</button>
            <button type="button" class="btn-secondary" onclick="toggleVisible(false)">Deselect Visible</button>
            <button type="submit" class="btn" style="padding: 7px 14px; font-size: 0.85rem;" onclick="document.getElementById('actionType').value='apply_channels'">Apply Changes (<span id="btnActiveCount">{{.ActiveCount}}</span>)</button>
          </div>
        </div>

        {{if .Channels}}
        <div class="search-bar">
          <input type="text" id="channelSearch" placeholder="🔍 Search channels by name, number, region, or EPG ID..." oninput="filterTable()" onkeydown="if(event.key==='Enter'){event.preventDefault();}">
          <select id="regionFilter" onchange="filterTable()">
            <option value="ALL">All Regions</option>
            <option value="Australia">Australia</option>
            <option value="United States">United States</option>
            <option value="Canada">Canada</option>
            <option value="Japan">Japan</option>
            <option value="United Kingdom">United Kingdom</option>
          </select>
          <select id="statusFilter" onchange="filterTable()">
            <option value="ALL">All Stations</option>
            <option value="ACTIVE">Selected Only (Active)</option>
            <option value="INACTIVE">Unselected Only</option>
          </select>
        </div>

        <div class="search-meta">
          <span id="visibleCountText">Showing all {{len .Channels}} stations</span>
          <button type="button" class="btn-secondary" style="padding: 3px 8px; font-size: 0.75rem;" onclick="clearSearchFilters()">Reset Search Filters</button>
        </div>

        <div class="table-container">
          <table class="channel-table">
            <thead>
              <tr>
                <th style="width: 40px; text-align: center;">Active</th>
                <th style="width: 72px;">#</th>
                <th>Channel Name</th>
                <th>Region</th>
                <th>EPG ID</th>
                <th>Test</th>
              </tr>
            </thead>
            <tbody id="channelTableBody">
              {{range .Channels}}
              <tr class="ch-row" data-key="{{.Key}}" data-url="{{.URL}}" data-name="{{.GuideName}}" data-group="{{.Group}}" data-epg="{{.TvgID}}">
                <td style="text-align: center;">
                  <input type="checkbox" class="ch-cb" value="{{.Key}}" {{if .Enabled}}checked{{end}} onchange="updateLiveCount()">
                </td>
                <td>
                  <input type="text" class="num-input ch-num-input" value="{{.GuideNumber}}" onkeydown="if(event.key==='Enter'){event.preventDefault();}">
                </td>
                <td><strong>{{.GuideName}}</strong></td>
                <td><span class="group-pill">{{.Group}}</span></td>
                <td style="color: var(--text-muted); font-size: 0.8rem;">{{if .TvgID}}{{.TvgID}}{{else}}-{{end}}</td>
                <td><a href="/stream/{{.GuideNumber}}" target="_blank" style="color: var(--accent); text-decoration: none;">Play</a></td>
              </tr>
              {{end}}
            </tbody>
          </table>
        </div>
        {{else}}
          <p style="color: var(--text-muted); font-size: 0.9rem;">No channels loaded. Enter your playlist URLs above and click Save &amp; Reload Feeds.</p>
        {{end}}
      </div>
    </form>

    <form id="importForm" action="/import-config" method="POST" enctype="multipart/form-data" style="display: none;">
      <input type="file" id="importFile" name="config_file" accept=".json" onchange="document.getElementById('importForm').submit()">
    </form>

    <div class="card" style="border-left: 4px solid var(--accent);">
      <h2>Optional Settings</h2>
      <p style="color: var(--text); font-size: 0.95rem; font-weight: 500; line-height: 1.5; margin-bottom: 10px;">
        Would you like your Media Server to display clean category tabs (<strong>Australia</strong>, <strong>United States</strong>, <strong>Canada</strong>, <strong>Japan</strong>) across the top of your Live TV screen instead of one giant mixed list?
      </p>
      <p style="color: var(--text-muted); font-size: 0.85rem; line-height: 1.5;">
        <strong>What this section does:</strong> By default, StreamTuner emulates a hardware HDHomeRun tuner. While HDHomeRun is plug-and-play, the HDHomeRun specification does not support category folders—so every station is placed into a single list. StreamTuner also generates a dedicated <strong>M3U Playlist endpoint</strong> that embeds regional category tags (<code>group-title</code>) alongside your repaired video streams.
      </p>
      <ul class="info-list">
        <li><strong>For Jellyfin / Emby Users:</strong> Instead of adding StreamTuner as an HDHomeRun device, you can add it as an <strong>M3U Tuner</strong> using the links on the next screen. Jellyfin will automatically read the embedded country tags and display filterable category tabs at the top of your Live TV guide.</li>
        <li><strong>For Plex Users:</strong> Plex DVR only accepts HDHomeRun connections and does not natively support M3U URLs or category tabs. Plex users should remain in standard HDHomeRun mode, where StreamTuner automatically separates countries by channel number blocks (1–99 for Australia, 1000+ for US, 2000+ for Canada, 3000+ for Japan).</li>
      </ul>
      <a href="/m3u-mode" class="btn btn-secondary">Open M3U Category Setup &amp; Endpoints &rarr;</a>
    </div>
  </div>

    <script>
    function updateLiveCount() {
      const allCbs = document.querySelectorAll('.ch-cb');
      let active = 0;
      allCbs.forEach(cb => {
        if (cb.checked) active++;
      });
      const statEl = document.getElementById('liveActiveStat');
      const btnEl = document.getElementById('btnActiveCount');
      if (statEl) statEl.textContent = active;
      if (btnEl) btnEl.textContent = active;
    }

    function toggleVisible(status) {
      const rows = document.querySelectorAll('.ch-row');
      rows.forEach(row => {
        if (row.style.display !== 'none') {
          const cb = row.querySelector('.ch-cb');
          if (cb) cb.checked = status;
        }
      });
      updateLiveCount();
    }

    function filterTable() {
      const searchInput = document.getElementById('channelSearch');
      const regionSelect = document.getElementById('regionFilter');
      const statusSelect = document.getElementById('statusFilter');
      if (!searchInput) return;

      const query = searchInput.value.toLowerCase().trim();
      const region = regionSelect ? regionSelect.value : 'ALL';
      const status = statusSelect ? statusSelect.value : 'ALL';

      const rows = document.querySelectorAll('.ch-row');
      let visible = 0;

      rows.forEach(row => {
        const name = (row.getAttribute('data-name') || '').toLowerCase();
        const group = row.getAttribute('data-group') || '';
        const epg = (row.getAttribute('data-epg') || '').toLowerCase();
        const numInput = row.querySelector('.ch-num-input');
        const num = numInput ? numInput.value.toLowerCase() : '';
        const cb = row.querySelector('.ch-cb');
        const isChecked = cb ? cb.checked : false;

        const matchesQuery = !query || name.includes(query) || num.includes(query) || group.toLowerCase().includes(query) || epg.includes(query);
        const matchesRegion = (region === 'ALL') || (group === region);
        const matchesStatus = (status === 'ALL') || (status === 'ACTIVE' && isChecked) || (status === 'INACTIVE' && !isChecked);

        if (matchesQuery && matchesRegion && matchesStatus) {
          row.style.display = '';
          visible++;
        } else {
          row.style.display = 'none';
        }
      });

      const countText = document.getElementById('visibleCountText');
      if (countText) {
        countText.textContent = 'Showing ' + visible + ' of ' + rows.length + ' stations';
      }
    }

    function clearSearchFilters() {
      const searchInput = document.getElementById('channelSearch');
      const regionSelect = document.getElementById('regionFilter');
      const statusSelect = document.getElementById('statusFilter');
      if (searchInput) searchInput.value = '';
      if (regionSelect) regionSelect.value = 'ALL';
      if (statusSelect) statusSelect.value = 'ALL';
      filterTable();
    }

    function prepareSubmission() {
      const rows = document.querySelectorAll('.ch-row');
      const state = [];
      rows.forEach(row => {
        const key = row.getAttribute('data-key') || '';
        const url = row.getAttribute('data-url') || '';
        const cb = row.querySelector('.ch-cb');
        const numInput = row.querySelector('.ch-num-input');
        state.push({
          key: key,
          url: url,
          num: numInput ? numInput.value.trim() : '',
          enabled: cb ? cb.checked : false
        });
      });
      const hiddenInput = document.getElementById('channelStateJSON');
      if (hiddenInput) {
        hiddenInput.value = JSON.stringify(state);
      }
    }

    function updateThemeBtn() {
      const current = document.documentElement.getAttribute('data-theme') || 'dark';
      const btn = document.getElementById('themeToggle');
      if (btn) {
        btn.textContent = current === 'dark' ? '☀️ Daytime Theme' : '🌙 Nighttime Theme';
      }
    }

    function toggleTheme() {
      const current = document.documentElement.getAttribute('data-theme') || 'dark';
      const next = current === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', next);
      localStorage.setItem('st_theme', next);
      updateThemeBtn();
    }

    updateThemeBtn();
  </script>
</body>
</html>`

const m3uModeTemplate = `<!DOCTYPE html>
<html lang="en" data-theme="dark">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>StreamTuner - Optional M3U Category Setup</title>
  <script>
    (function() {
      const savedTheme = localStorage.getItem('st_theme') || 'dark';
      document.documentElement.setAttribute('data-theme', savedTheme);
    })();
  </script>
  <style>
    :root {
      --bg: #0e131f;
      --card: #182032;
      --input-bg: #0c101a;
      --border: #243049;
      --accent: #00d2c4;
      --btn-text: #051015;
      --text: #f0f4f8;
      --text-muted: #8e9eb5;
    }
    html[data-theme="light"] {
      --bg: #f1f5f9;
      --card: #ffffff;
      --input-bg: #f8fafc;
      --border: #cbd5e1;
      --accent: #0d9488;
      --btn-text: #ffffff;
      --text: #0f172a;
      --text-muted: #475569;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    body { background: var(--bg); color: var(--text); padding: 16px; display: flex; justify-content: center; }
    .container { width: 100%; max-width: 800px; }
    header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; padding-bottom: 14px; border-bottom: 1px solid var(--border); flex-wrap: wrap; gap: 10px; }
    h1 { font-size: 1.4rem; color: var(--accent); }
    .card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 22px; margin-bottom: 20px; line-height: 1.5; }
    .url-box { background: var(--input-bg); border: 1px solid var(--border); padding: 12px; border-radius: 8px; font-family: monospace; font-size: 0.9rem; color: var(--accent); word-break: break-all; margin: 8px 0 16px 0; }
    .btn { background: var(--accent); color: var(--btn-text); border: none; padding: 10px 18px; font-size: 0.9rem; font-weight: 700; border-radius: 8px; text-decoration: none; display: inline-block; }
    .step { margin-bottom: 16px; font-size: 0.9rem; }
    .step strong { color: var(--accent); }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1>M3U Category Mode &amp; Endpoints</h1>
      <a href="/" class="btn" style="padding: 6px 12px; font-size: 0.8rem;">&larr; Back to Dashboard</a>
    </header>

    <div class="card">
      <h2 style="font-size: 1.2rem; margin-bottom: 14px;">StreamTuner Direct Endpoints</h2>

      <div style="font-size: 0.85rem; color: var(--text-muted);">M3U Playlist URL (Includes Category Group Tags for Jellyfin/Emby):</div>
      <div class="url-box">{{.BaseURL}}/channels.m3u</div>

      <div style="font-size: 0.85rem; color: var(--text-muted);">Aggregated XMLTV EPG URL (Filtered to Active Stations Only):</div>
      <div class="url-box">{{.BaseURL}}/epg.xml</div>

      <div style="font-size: 0.85rem; color: var(--text-muted);">HDHomeRun Lineup URL (Standard Mode for Plex/Jellyfin):</div>
      <div class="url-box">{{.BaseURL}}/lineup.json</div>
    </div>

    <div class="card">
      <h2 style="font-size: 1.2rem; margin-bottom: 14px;">How to Enable Category Tabs in Jellyfin</h2>
      <div class="step">
        <strong>1. Remove Existing HDHomeRun Tuner:</strong> In Jellyfin, go to <em>Dashboard &gt; Live TV</em>. Under <em>Tuner Devices</em>, delete the existing StreamTuner HDHomeRun entry.
      </div>
      <div class="step">
        <strong>2. Add as M3U Tuner:</strong> Click <strong>+</strong> next to Tuner Devices and select <strong>M3U Tuner</strong>.
      </div>
      <div class="step">
        <strong>3. Paste Playlist Link:</strong> In the <em>File or URL</em> box, paste:
        <div style="font-family: monospace; color: var(--accent); margin-top: 4px;">{{.BaseURL}}/channels.m3u</div>
      </div>
      <div class="step">
        <strong>4. Set Simultaneous Stream Limit:</strong> Set the limit to match your Virtual Tuners count.
      </div>
      <div class="step">
        <strong>5. Connect Guide:</strong> Under <em>TV Guide Data Providers</em>, ensure your XMLTV provider points to:
        <div style="font-family: monospace; color: var(--accent); margin-top: 4px;">{{.BaseURL}}/epg.xml</div>
      </div>
      <div class="step">
        <strong>6. Done!</strong> Open Live TV in Jellyfin and use the category filter at the top of the guide to switch between countries.
      </div>
    </div>
  </div>
</body>
</html>`

func getPlaylistMeta(url string, index int) (tag string, group string, baseOffset int) {
	u := strings.ToLower(url)
	if strings.Contains(u, "/au/") || strings.Contains(u, "australia") || strings.Contains(u, "raw-tv") {
		return "[AU]", "Australia", 1
	} else if strings.Contains(u, "/us.m3u") || strings.Contains(u, "/us/") || strings.Contains(u, "countries/us") {
		return "[US]", "United States", 1000
	} else if strings.Contains(u, "/ca.m3u") || strings.Contains(u, "/ca/") || strings.Contains(u, "countries/ca") {
		return "[CA]", "Canada", 2000
	} else if strings.Contains(u, "/jp.m3u") || strings.Contains(u, "/jp/") || strings.Contains(u, "countries/jp") {
		return "[JP]", "Japan", 3000
	} else if strings.Contains(u, "/uk.m3u") || strings.Contains(u, "/uk/") || strings.Contains(u, "countries/uk") {
		return "[UK]", "United Kingdom", 4000
	}
	return fmt.Sprintf("[P%d]", index+1), fmt.Sprintf("Playlist %d", index+1), (index+5)*1000
}

func fetchAndParseM3U(url string, tag string, group string, baseOffset int) ([]Channel, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed []Channel
	scanner := bufio.NewScanner(resp.Body)
	var currentName, currentLogo, currentTvgID, currentGroup string
	chCounter := 1

	reTvgID := regexp.MustCompile(`tvg-id="([^"]+)"`)
	reTvgName := regexp.MustCompile(`tvg-name="([^"]+)"`)
	reTvgLogo := regexp.MustCompile(`tvg-logo="([^"]+)"`)
	reTvgChNo := regexp.MustCompile(`tvg-chno="([^"]+)"`)
	reGroup := regexp.MustCompile(`group-title="([^"]+)"`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXTINF:") {
			currentName = ""
			currentLogo = ""
			currentTvgID = ""
			currentGroup = group

			if m := reTvgID.FindStringSubmatch(line); len(m) > 1 {
				currentTvgID = m[1]
			}
			if m := reTvgName.FindStringSubmatch(line); len(m) > 1 {
				currentName = m[1]
			}
			if m := reTvgLogo.FindStringSubmatch(line); len(m) > 1 {
				currentLogo = m[1]
			}
			if m := reGroup.FindStringSubmatch(line); len(m) > 1 && group == "" {
				currentGroup = m[1]
			}
			if currentName == "" {
				parts := strings.Split(line, ",")
				if len(parts) > 1 {
					currentName = strings.TrimSpace(parts[len(parts)-1])
				}
			}

			if !strings.HasPrefix(currentName, "[") {
				currentName = fmt.Sprintf("%s %s", tag, currentName)
			}

			guideNo := fmt.Sprintf("%d", baseOffset+chCounter-1)
			if m := reTvgChNo.FindStringSubmatch(line); len(m) > 1 {
				parsedNo, err := strconv.Atoi(m[1])
				if err == nil {
					if baseOffset == 1 {
						guideNo = fmt.Sprintf("%d", parsedNo)
					} else {
						guideNo = fmt.Sprintf("%d", baseOffset+parsedNo)
					}
				}
			}

			parsed = append(parsed, Channel{
				GuideNumber: guideNo,
				GuideName:   currentName,
				Logo:        currentLogo,
				TvgID:       currentTvgID,
				Group:       currentGroup,
				Enabled:     true,
			})
			chCounter++
		} else if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			if len(parsed) > 0 && parsed[len(parsed)-1].URL == "" {
				parsed[len(parsed)-1].URL = line
				parsed[len(parsed)-1].Key = makeChannelKey(parsed[len(parsed)-1].GuideName, line)
			}
		}
	}

	var cleaned []Channel
	for _, c := range parsed {
		if c.URL != "" {
			cleaned = append(cleaned, c)
		}
	}
	return cleaned, nil
}

func parseCustomChannels(text string) []Channel {
	var list []Channel
	lines := strings.Split(text, "\n")
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.Split(trimmed, ",")
		if len(parts) >= 3 {
			guideNo := strings.TrimSpace(parts[0])
			name := strings.TrimSpace(parts[1])
			streamURL := strings.TrimSpace(parts[2])
			tvgID := ""
			if len(parts) >= 4 {
				tvgID = strings.TrimSpace(parts[3])
			}
			if !strings.HasPrefix(name, "[") {
				name = "[AU] " + name
			}
			list = append(list, Channel{
				Key:         makeChannelKey(name, streamURL),
				GuideNumber: guideNo,
				GuideName:   name,
				URL:         streamURL,
				TvgID:       tvgID,
				Group:       "Australia",
				Enabled:     true,
			})
		}
	}
	return list
}

func matchesFilter(name string, filterKeywords string) bool {
	if strings.TrimSpace(filterKeywords) == "" {
		return true
	}
	keywords := strings.Split(filterKeywords, ",")
	lowerName := strings.ToLower(name)
	for _, kw := range keywords {
		cleanKw := strings.ToLower(strings.TrimSpace(kw))
		if cleanKw != "" && strings.Contains(lowerName, cleanKw) {
			return true
		}
	}
	return false
}

func sortChannels(list []Channel) {
	sort.Slice(list, func(i, j int) bool {
		numI, errI := strconv.Atoi(list[i].GuideNumber)
		numJ, errJ := strconv.Atoi(list[j].GuideNumber)
		if errI == nil && errJ == nil {
			if numI != numJ {
				return numI < numJ
			}
		} else if list[i].GuideNumber != list[j].GuideNumber {
			return list[i].GuideNumber < list[j].GuideNumber
		}
		return list[i].GuideName < list[j].GuideName
	})
}

func applySavedChannelState(list []Channel) {
	for i := range list {
		k := list[i].Key
		u := list[i].URL

		if appConfig.CustomNumbers != nil {
			if overrideNo, ok := appConfig.CustomNumbers[k]; ok && overrideNo != "" {
				list[i].GuideNumber = overrideNo
			} else if overrideNo, ok := appConfig.CustomNumbers[u]; ok && overrideNo != "" {
				list[i].GuideNumber = overrideNo
			}
		}

		if appConfig.EnabledChannels != nil && (appConfig.EnabledChannels[k] || appConfig.EnabledChannels[u]) {
			list[i].Enabled = true
		} else if appConfig.DisabledChannels != nil && (appConfig.DisabledChannels[k] || appConfig.DisabledChannels[u]) {
			list[i].Enabled = false
		} else if strings.TrimSpace(appConfig.FilterKeywords) != "" {
			list[i].Enabled = matchesFilter(list[i].GuideName, appConfig.FilterKeywords)
		} else {
			list[i].Enabled = true
		}
	}
}

func loadAllChannels() {
	var combined []Channel

	if appConfig.M3UURLs != "" {
		urls := strings.Split(appConfig.M3UURLs, "\n")
		for i, u := range urls {
			trimmed := strings.TrimSpace(u)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			tag, group, baseOffset := getPlaylistMeta(trimmed, i)
			chList, err := fetchAndParseM3U(trimmed, tag, group, baseOffset)
			if err != nil {
				log.Printf("Error fetching playlist %s: %v", trimmed, err)
				continue
			}
			combined = append(combined, chList...)
		}
	}

	customs := parseCustomChannels(appConfig.CustomChannels)
	for _, cust := range customs {
		replaced := false
		for i := range combined {
			if combined[i].Key == cust.Key || combined[i].URL == cust.URL {
				combined[i] = cust
				replaced = true
				break
			}
		}
		if !replaced {
			combined = append([]Channel{cust}, combined...)
		}
	}

	applySavedChannelState(combined)
	sortChannels(combined)
	channels = combined
}

func refreshEPG() {
	epgLock.Lock()
	if isRefreshingEPG {
		epgLock.Unlock()
		return
	}
	isRefreshingEPG = true
	epgLock.Unlock()

	defer func() {
		epgLock.Lock()
		isRefreshingEPG = false
		epgLock.Unlock()
	}()

	configLock.RLock()
	epgURLs := strings.Split(appConfig.XMLTVURLs, "\n")
	activeTvgIDs := make(map[string]string)
	activeNames := make(map[string]string)

	for _, c := range channels {
		if c.Enabled {
			if c.TvgID != "" {
				activeTvgIDs[c.TvgID] = c.TvgID
			}
			cleanName := strings.ToLower(strings.TrimSpace(c.GuideName))
			if cleanName != "" {
				targetID := c.TvgID
				if targetID == "" {
					targetID = c.GuideNumber
				}
				activeNames[cleanName] = targetID
			}
		}
	}
	configLock.RUnlock()

	if len(activeTvgIDs) == 0 && len(activeNames) == 0 {
		return
	}

	tmpFile := epgPath + ".tmp"
	out, err := os.Create(tmpFile)
	if err != nil {
		log.Printf("EPG create error: %v", err)
		return
	}

	out.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	out.WriteString("<!DOCTYPE tv SYSTEM \"xmltv.dtd\">\n")
	out.WriteString("<tv generator-info-name=\"StreamTuner\">\n")

	configLock.RLock()
	for _, c := range channels {
		if !c.Enabled {
			continue
		}
		chID := c.TvgID
		if chID == "" {
			chID = c.GuideNumber
		}
		out.WriteString(fmt.Sprintf("  <channel id=\"%s\">\n", template.HTMLEscapeString(chID)))
		out.WriteString(fmt.Sprintf("    <display-name>%s</display-name>\n", template.HTMLEscapeString(c.GuideName)))
		out.WriteString(fmt.Sprintf("    <display-name>%s</display-name>\n", template.HTMLEscapeString(c.GuideNumber)))
		if c.Logo != "" {
			out.WriteString(fmt.Sprintf("    <icon src=\"%s\"/>\n", template.HTMLEscapeString(c.Logo)))
		}
		out.WriteString("  </channel>\n")
	}
	configLock.RUnlock()

	client := &http.Client{Timeout: 3 * time.Minute}

	for _, u := range epgURLs {
		trimmed := strings.TrimSpace(u)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		log.Printf("Fetching EPG: %s", trimmed)
		req, err := http.NewRequest("GET", trimmed, nil)
		if err != nil {
			log.Printf("EPG request error: %v", err)
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("EPG download error for %s: %v", trimmed, err)
			continue
		}

		var reader io.Reader = resp.Body
		if strings.HasSuffix(strings.ToLower(trimmed), ".gz") || resp.Header.Get("Content-Encoding") == "gzip" {
			gz, err := gzip.NewReader(resp.Body)
			if err == nil {
				defer gz.Close()
				reader = gz
			}
		}

		matchedIDs := make(map[string]string)
		dec := xml.NewDecoder(reader)

		for {
			token, err := dec.Token()
			if err != nil {
				break
			}
			switch se := token.(type) {
			case xml.StartElement:
				if se.Name.Local == "channel" {
					var ch struct {
						ID          string   `xml:"id,attr"`
						DisplayName []string `xml:"display-name"`
					}
					if err := dec.DecodeElement(&ch, &se); err == nil {
						if targetID, ok := activeTvgIDs[ch.ID]; ok {
							matchedIDs[ch.ID] = targetID
						} else {
							for _, dn := range ch.DisplayName {
								clean := strings.ToLower(strings.TrimSpace(dn))
								if targetID, ok := activeNames[clean]; ok {
									matchedIDs[ch.ID] = targetID
									break
								}
							}
						}
					}
				} else if se.Name.Local == "programme" {
					var prog struct {
						Start   string `xml:"start,attr"`
						Stop    string `xml:"stop,attr"`
						Channel string `xml:"channel,attr"`
						Raw     []byte `xml:",innerxml"`
					}
					if err := dec.DecodeElement(&prog, &se); err == nil {
						if targetID, ok := matchedIDs[prog.Channel]; ok {
							fmt.Fprintf(out, "  <programme start=\"%s\" stop=\"%s\" channel=\"%s\">%s</programme>\n",
								prog.Start, prog.Stop, template.HTMLEscapeString(targetID), string(prog.Raw))
						}
					}
				}
			}
		}
		resp.Body.Close()
	}

	out.WriteString("</tv>\n")
	out.Close()
	os.Rename(tmpFile, epgPath)
	log.Printf("EPG aggregation complete: saved to %s", epgPath)
}

func loadConfig() {
	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" {
		configDir = "/config"
	}
	os.MkdirAll(configDir, 0755)
	configPath = filepath.Join(configDir, "settings.json")
	epgPath = filepath.Join(configDir, "epg.xml")

	appConfig = Config{
		FriendlyName:     "StreamTuner",
		TunerCount:       4,
		M3UURLs:          "",
		CustomChannels:   "",
		XMLTVURLs:        "",
		FilterKeywords:   "",
		EnabledChannels:  make(map[string]bool),
		DisabledChannels: make(map[string]bool),
		CustomNumbers:    make(map[string]string),
	}

	data, err := os.ReadFile(configPath)
	if err == nil {
		json.Unmarshal(data, &appConfig)
	}

	if appConfig.FriendlyName == "" {
		appConfig.FriendlyName = "StreamTuner"
	}
	if appConfig.XMLTVURLs == "" && appConfig.XMLTVURL != "" {
		appConfig.XMLTVURLs = appConfig.XMLTVURL
	}
	if appConfig.EnabledChannels == nil {
		appConfig.EnabledChannels = make(map[string]bool)
	}
	if appConfig.DisabledChannels == nil {
		appConfig.DisabledChannels = make(map[string]bool)
	}
	if appConfig.CustomNumbers == nil {
		appConfig.CustomNumbers = make(map[string]string)
	}

	loadAllChannels()
}

func saveConfig(cfg Config) error {
	configLock.Lock()
	defer configLock.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

func getBaseURL(r *http.Request) string {
	host := r.Host
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	return fmt.Sprintf("%s://%s", proto, host)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	loadConfig()

	go func() {
		time.Sleep(5 * time.Second)
		refreshEPG()
		ticker := time.NewTicker(12 * time.Hour)
		for range ticker.C {
			refreshEPG()
		}
	}()

	// Dashboard
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		tmpl, _ := template.New("ui").Parse(uiTemplate)
		configLock.RLock()
		active := 0
		for _, c := range channels {
			if c.Enabled {
				active++
			}
		}
		data := struct {
			Version     string
			Config      Config
			Channels    []Channel
			ActiveCount int
			Saved       bool
		}{
			Version:     appVersion,
			Config:      appConfig,
			Channels:    channels,
			ActiveCount: active,
			Saved:       r.URL.Query().Get("saved") == "1",
		}
		configLock.RUnlock()
		tmpl.Execute(w, data)
	})

	// M3U Mode Setup Screen
	http.HandleFunc("/m3u-mode", func(w http.ResponseWriter, r *http.Request) {
		tmpl, _ := template.New("m3u").Parse(m3uModeTemplate)
		baseURL := getBaseURL(r)
		data := struct {
			BaseURL string
		}{
			BaseURL: baseURL,
		}
		tmpl.Execute(w, data)
	})

	// Export Config
	http.HandleFunc("/export-config", func(w http.ResponseWriter, r *http.Request) {
		configLock.RLock()
		data, err := json.MarshalIndent(appConfig, "", "  ")
		configLock.RUnlock()
		if err != nil {
			http.Error(w, "Failed to export config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", "attachment; filename=\"streamtuner_backup.json\"")
		w.Write(data)
	})

	// Import Config
	http.HandleFunc("/import-config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		file, _, err := r.FormFile("config_file")
		if err != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err == nil {
			var newCfg Config
			if err := json.Unmarshal(data, &newCfg); err == nil {
				if newCfg.FriendlyName == "" {
					newCfg.FriendlyName = "StreamTuner"
				}
				configLock.Lock()
				appConfig = newCfg
				loadAllChannels()
				cfgCopy := appConfig
				configLock.Unlock()
				saveConfig(cfgCopy)
				go refreshEPG()
			}
		}
		http.Redirect(w, r, "/?saved=1", http.StatusSeeOther)
	})

	// Save Settings & Channel Selections
	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
		if err := r.ParseForm(); err != nil {
			log.Printf("Form parse error: %v", err)
		}

		tCount, _ := strconv.Atoi(r.FormValue("tuner_count"))
		if tCount < 1 {
			tCount = 1
		}

		actionType := r.FormValue("action_type")
		stateJSON := r.FormValue("channel_state_json")

		configLock.Lock()

		newEnabled := make(map[string]bool)
		newDisabled := make(map[string]bool)
		newCustomNumbers := make(map[string]string)

		if appConfig.CustomNumbers != nil {
			for k, v := range appConfig.CustomNumbers {
				newCustomNumbers[k] = v
			}
		}

		var updates []ChannelStateUpdate
		if stateJSON != "" && json.Unmarshal([]byte(stateJSON), &updates) == nil && len(updates) > 0 {
			updateMap := make(map[string]ChannelStateUpdate, len(updates))
			for _, u := range updates {
				updateMap[u.Key] = u
				if u.Num != "" {
					newCustomNumbers[u.Key] = u.Num
					if u.URL != "" {
						newCustomNumbers[u.URL] = u.Num
					}
				}
				if u.Enabled {
					newEnabled[u.Key] = true
					if u.URL != "" {
						newEnabled[u.URL] = true
					}
				} else {
					newDisabled[u.Key] = true
					if u.URL != "" {
						newDisabled[u.URL] = true
					}
				}
			}

			for i := range channels {
				if u, ok := updateMap[channels[i].Key]; ok {
					channels[i].Enabled = u.Enabled
					if u.Num != "" {
						channels[i].GuideNumber = u.Num
					}
				}
			}
		} else {
			for _, c := range channels {
				if c.Enabled {
					newEnabled[c.Key] = true
					newEnabled[c.URL] = true
				} else {
					newDisabled[c.Key] = true
					newDisabled[c.URL] = true
				}
			}
		}

		newM3U := strings.TrimSpace(r.FormValue("m3u_urls"))
		newCustom := strings.TrimSpace(r.FormValue("custom_channels"))
		newFilter := strings.TrimSpace(r.FormValue("filter_keywords"))
		sourcesChanged := (newM3U != appConfig.M3UURLs) || (newCustom != appConfig.CustomChannels) || (newFilter != appConfig.FilterKeywords)

		appConfig.FriendlyName = "StreamTuner"
		appConfig.TunerCount = tCount
		appConfig.M3UURLs = newM3U
		appConfig.CustomChannels = newCustom
		appConfig.XMLTVURLs = strings.TrimSpace(r.FormValue("xmltv_urls"))
		appConfig.FilterKeywords = newFilter
		appConfig.EnabledChannels = newEnabled
		appConfig.DisabledChannels = newDisabled
		appConfig.CustomNumbers = newCustomNumbers

		if actionType == "reload_feeds" || sourcesChanged || len(channels) == 0 {
			loadAllChannels()
		} else {
			sortChannels(channels)
		}

		cfgCopy := appConfig
		configLock.Unlock()

		if err := saveConfig(cfgCopy); err != nil {
			log.Printf("Failed to save settings.json: %v", err)
		}
		go refreshEPG()

		http.Redirect(w, r, "/?saved=1", http.StatusSeeOther)
	})

	// M3U Playlist Endpoint
	http.HandleFunc("/channels.m3u", func(w http.ResponseWriter, r *http.Request) {
		configLock.RLock()
		defer configLock.RUnlock()

		baseURL := getBaseURL(r)
		w.Header().Set("Content-Type", "application/x-mpegURL; charset=utf-8")
		w.Header().Set("Content-Disposition", "inline; filename=\"channels.m3u\"")

		fmt.Fprintln(w, "#EXTM3U")
		for _, c := range channels {
			if c.Enabled {
				chID := c.TvgID
				if chID == "" {
					chID = c.GuideNumber
				}
				fmt.Fprintf(w, "#EXTINF:-1 tvg-id=\"%s\" tvg-name=\"%s\" tvg-chno=\"%s\" tvg-logo=\"%s\" group-title=\"%s\",%s\n",
					chID, c.GuideName, c.GuideNumber, c.Logo, c.Group, c.GuideName)
				fmt.Fprintf(w, "%s/stream/%s\n", baseURL, c.GuideNumber)
			}
		}
	})

	// Aggregated EPG Endpoint
	http.HandleFunc("/epg.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		if _, err := os.Stat(epgPath); os.IsNotExist(err) {
			fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><tv generator-info-name="StreamTuner"></tv>`)
			return
		}
		http.ServeFile(w, r, epgPath)
	})

	// HDHomeRun Discovery
	http.HandleFunc("/discover.json", func(w http.ResponseWriter, r *http.Request) {
		configLock.RLock()
		defer configLock.RUnlock()

		baseURL := getBaseURL(r)
		info := DeviceInfo{
			FriendlyName:    "StreamTuner",
			ModelNumber:     "HDTC-2US",
			FirmwareName:    "hdhomeruntc_atsc",
			FirmwareVersion: "20260101",
			DeviceID:        "104E32B1",
			DeviceAuth:      "streamtuner123",
			TunerCount:      appConfig.TunerCount,
			BaseURL:         baseURL,
			LineupURL:       fmt.Sprintf("%s/lineup.json", baseURL),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	})

	http.HandleFunc("/lineup_status.json", func(w http.ResponseWriter, r *http.Request) {
		status := LineupStatus{
			ScanInProgress: 0,
			ScanPossible:   1,
			Source:         "Cable",
			SourceList:     []string{"Cable"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	})

	// Lineup JSON
	http.HandleFunc("/lineup.json", func(w http.ResponseWriter, r *http.Request) {
		configLock.RLock()
		defer configLock.RUnlock()

		baseURL := getBaseURL(r)
		var lineup []LineupItem
		for _, c := range channels {
			if c.Enabled {
				lineup = append(lineup, LineupItem{
					GuideNumber: c.GuideNumber,
					GuideName:   c.GuideName,
					URL:         fmt.Sprintf("%s/stream/%s", baseURL, c.GuideNumber),
				})
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if len(lineup) == 0 {
			json.NewEncoder(w).Encode([]LineupItem{})
			return
		}
		json.NewEncoder(w).Encode(lineup)
	})

	// Stream Proxy
	http.HandleFunc("/stream/", func(w http.ResponseWriter, r *http.Request) {
		chNum := strings.TrimPrefix(r.URL.Path, "/stream/")
		var targetURL string

		configLock.RLock()
		for _, c := range channels {
			if c.GuideNumber == chNum {
				targetURL = c.URL
				break
			}
		}
		configLock.RUnlock()

		if targetURL == "" {
			http.Error(w, "Channel not found", http.StatusNotFound)
			return
		}

		log.Printf("Streaming channel %s from %s", chNum, targetURL)

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		cmd := exec.CommandContext(ctx, "ffmpeg",
			"-hide_banner",
			"-loglevel", "warning",
			"-probesize", "1000000",
			"-analyzeduration", "1000000",
			"-user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
			"-i", targetURL,
			"-c", "copy",
			"-f", "mpegts",
			"-flush_packets", "1",
			"pipe:1",
		)

		cmd.Stderr = os.Stderr

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("FFmpeg stdout pipe error: %v", err)
			http.Error(w, "Transcoder pipe error", http.StatusInternalServerError)
			return
		}

		if err := cmd.Start(); err != nil {
			log.Printf("FFmpeg failed to start: %v", err)
			http.Error(w, "Failed to start stream", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Connection", "keep-alive")

		io.Copy(w, stdout)
		cmd.Process.Kill()
		cmd.Wait()
	})

	log.Printf("StreamTuner %s running on port %s", appVersion, port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %s\n", err)
	}
}
