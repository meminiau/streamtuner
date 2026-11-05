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

type Config struct {
	FriendlyName     string            `json:"friendly_name"`
	TunerCount       int               `json:"tuner_count"`
	M3UURLs          string            `json:"m3u_urls"`
	CustomChannels   string            `json:"custom_channels"`
	XMLTVURLs        string            `json:"xmltv_urls"`
	XMLTVURL         string            `json:"xmltv_url,omitempty"`
	FilterKeywords   string            `json:"filter_keywords"`
	DisabledChannels map[string]bool   `json:"disabled_channels"`
	CustomNumbers    map[string]string `json:"custom_numbers"`
}

type Channel struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	Logo        string `json:"Logo,omitempty"`
	TvgID       string `json:"TvgID,omitempty"`
	Group       string `json:"Group,omitempty"`
	Enabled     bool   `json:"Enabled"`
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

const uiTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>StreamTuner Dashboard</title>
  <style>
    :root {
      --bg: #0e131f;
      --card: #182032;
      --border: #243049;
      --accent: #00d2c4;
      --accent-hover: #00b3a6;
      --text: #f0f4f8;
      --text-muted: #8e9eb5;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    body { background: var(--bg); color: var(--text); padding: 16px; display: flex; justify-content: center; }
    .container { width: 100%; max-width: 960px; }
    header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; padding-bottom: 14px; border-bottom: 1px solid var(--border); flex-wrap: wrap; gap: 10px; }
    .logo-group { display: flex; align-items: center; gap: 10px; }
    .logo-group h1 { font-size: 1.4rem; color: var(--accent); }
    .badge { background: var(--border); color: var(--accent); padding: 4px 10px; border-radius: 999px; font-size: 0.75rem; font-weight: 600; }
    .header-links { display: flex; gap: 12px; align-items: center; }
    .header-links a { color: var(--accent); font-size: 0.85rem; text-decoration: none; font-weight: 500; }
    .card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 20px; margin-bottom: 20px; box-shadow: 0 4px 20px rgba(0,0,0,0.3); }
    .card h2 { font-size: 1.15rem; margin-bottom: 14px; color: var(--text); }
    .form-group { margin-bottom: 14px; }
    label { display: block; margin-bottom: 6px; font-size: 0.85rem; color: var(--text-muted); font-weight: 500; }
    input[type="text"], input[type="number"], textarea { width: 100%; padding: 10px 12px; background: #0c101a; border: 1px solid var(--border); border-radius: 8px; color: var(--text); font-size: 0.95rem; }
    textarea { resize: vertical; min-height: 80px; font-family: monospace; font-size: 0.85rem; }
    input:focus, textarea:focus { outline: none; border-color: var(--accent); }
    .btn { background: var(--accent); color: #051015; border: none; padding: 10px 18px; font-size: 0.9rem; font-weight: 700; border-radius: 8px; cursor: pointer; transition: background 0.2s; text-decoration: none; display: inline-block; }
    .btn:hover { background: var(--accent-hover); }
    .btn-secondary { background: var(--border); color: var(--text); font-weight: 600; padding: 7px 12px; font-size: 0.8rem; border-radius: 6px; text-decoration: none; cursor: pointer; border: 1px solid var(--border); }
    .btn-secondary:hover { border-color: var(--accent); }
    .stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; margin-bottom: 20px; }
    .stat-box { background: var(--card); border: 1px solid var(--border); border-radius: 10px; padding: 14px; text-align: center; }
    .stat-box .label { font-size: 0.8rem; color: var(--text-muted); }
    .stat-box .val { font-size: 1.6rem; font-weight: 700; color: var(--accent); margin-top: 4px; }
    .table-actions { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex-wrap: wrap; gap: 8px; }
    
    /* Scrollable Channel Table Window */
    .table-container {
      max-height: 480px;
      overflow-y: auto;
      border: 1px solid var(--border);
      border-radius: 8px;
      background: #0c101a;
    }
    .channel-table { width: 100%; border-collapse: collapse; }
    .channel-table th, .channel-table td { padding: 8px 10px; text-align: left; border-bottom: 1px solid var(--border); font-size: 0.85rem; }
    .channel-table th {
      position: sticky;
      top: 0;
      background: #182032;
      z-index: 2;
      color: var(--text-muted);
      font-weight: 600;
      border-bottom: 2px solid var(--border);
    }
    .num-input {
      width: 58px;
      padding: 5px 6px;
      background: #182032;
      border: 1px solid var(--border);
      border-radius: 4px;
      color: var(--accent);
      font-weight: 700;
      text-align: center;
      font-size: 0.85rem;
    }
    .num-input:focus { border-color: var(--accent); outline: none; }
    .group-pill { background: var(--border); padding: 2px 6px; border-radius: 4px; font-size: 0.75rem; color: var(--text-muted); }
    input[type="checkbox"] { width: 18px; height: 18px; accent-color: var(--accent); cursor: pointer; }
    .backup-bar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px; padding-top: 10px; border-top: 1px solid var(--border); margin-top: 16px; }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <div class="logo-group">
        <h1>StreamTuner</h1>
        <span class="badge">v0.7.0</span>
      </div>
      <div class="header-links">
        <a href="/lineup.json" target="_blank">Lineup JSON</a>
        <a href="/epg.xml" target="_blank">Aggregated EPG XML</a>
      </div>
    </header>

    <div class="stats">
      <div class="stat-box">
        <div class="label">Virtual Tuners</div>
        <div class="val">{{.Config.TunerCount}}</div>
      </div>
      <div class="stat-box">
        <div class="label">Total Ingested</div>
        <div class="val">{{len .Channels}}</div>
      </div>
      <div class="stat-box">
        <div class="label">Active in Lineup</div>
        <div class="val">{{.ActiveCount}}</div>
      </div>
    </div>

    <form action="/save" method="POST">
      <div class="card">
        <h2>Configuration &amp; Sources</h2>
        <div class="form-group">
          <label>Friendly Name</label>
          <input type="text" name="friendly_name" value="{{.Config.FriendlyName}}" required>
        </div>
        <div class="form-group">
          <label>Virtual Tuners</label>
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
          <label>Channel Name Filter (Optional comma-separated keywords)</label>
          <input type="text" name="filter_keywords" value="{{.Config.FilterKeywords}}" placeholder="e.g.: ABC, Seven, Nine, 10, SBS, BBC">
        </div>
        
        <div style="display: flex; gap: 10px; align-items: center; flex-wrap: wrap;">
          <button type="submit" class="btn">Save &amp; Reload Feeds</button>
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
          <h2>Channel Lineup ({{len .Channels}})</h2>
          <div style="display: flex; gap: 8px;">
            <button type="button" class="btn-secondary" onclick="toggleAll(true)">Select All</button>
            <button type="button" class="btn-secondary" onclick="toggleAll(false)">Deselect All</button>
            <button type="submit" class="btn" style="padding: 6px 14px; font-size: 0.8rem;">Apply Changes</button>
          </div>
        </div>

        {{if .Channels}}
        <div class="table-container">
          <table class="channel-table">
            <thead>
              <tr>
                <th style="width: 40px; text-align: center;">Active</th>
                <th style="width: 70px;">#</th>
                <th>Channel Name</th>
                <th>Region</th>
                <th>EPG ID</th>
                <th>Test</th>
              </tr>
            </thead>
            <tbody>
              {{range .Channels}}
              <tr>
                <td style="text-align: center;">
                  <input type="checkbox" name="active_channels" value="{{.URL}}" {{if .Enabled}}checked{{end}}>
                  <input type="hidden" name="ch_url" value="{{.URL}}">
                </td>
                <td>
                  <input type="text" class="num-input" name="ch_num" value="{{.GuideNumber}}">
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
          <p style="color: var(--text-muted); font-size: 0.9rem;">No channels loaded. Enter your playlist URLs above and click Save.</p>
        {{end}}
      </div>
    </form>

    <!-- Hidden form for config file restore -->
    <form id="importForm" action="/import-config" method="POST" enctype="multipart/form-data" style="display: none;">
      <input type="file" id="importFile" name="config_file" accept=".json" onchange="document.getElementById('importForm').submit()">
    </form>

    <!-- Option 4: Switch to M3U Tuner Mode -->
    <div class="card" style="border-left: 4px solid var(--accent);">
      <h2>Option 4: Setup Native Jellyfin Category Tabs (M3U Mode)</h2>
      <p style="color: var(--text-muted); font-size: 0.85rem; line-height: 1.5; margin-bottom: 12px;">
        Want Jellyfin to display clean category tabs (<strong>Australia</strong>, <strong>United States</strong>, <strong>Canada</strong>, <strong>Japan</strong>) across the top of your Live TV screen instead of one giant mixed list?
      </p>
      <a href="/m3u-mode" class="btn btn-secondary">Open M3U Tuner Setup &rarr;</a>
    </div>
  </div>

  <script>
    function toggleAll(status) {
      const boxes = document.querySelectorAll('input[name="active_channels"]');
      boxes.forEach(cb => cb.checked = status);
    }
  </script>
</body>
</html>`

const m3uModeTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>StreamTuner - M3U Tuner Mode Setup</title>
  <style>
    :root {
      --bg: #0e131f;
      --card: #182032;
      --border: #243049;
      --accent: #00d2c4;
      --text: #f0f4f8;
      --text-muted: #8e9eb5;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    body { background: var(--bg); color: var(--text); padding: 16px; display: flex; justify-content: center; }
    .container { width: 100%; max-width: 800px; }
    header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; padding-bottom: 14px; border-bottom: 1px solid var(--border); }
    h1 { font-size: 1.4rem; color: var(--accent); }
    .card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 22px; margin-bottom: 20px; line-height: 1.5; }
    .url-box { background: #0c101a; border: 1px solid var(--border); padding: 12px; border-radius: 8px; font-family: monospace; font-size: 0.9rem; color: var(--accent); word-break: break-all; margin: 8px 0 16px 0; }
    .btn { background: var(--accent); color: #051015; border: none; padding: 10px 18px; font-size: 0.9rem; font-weight: 700; border-radius: 8px; text-decoration: none; display: inline-block; }
    .step { margin-bottom: 16px; }
    .step strong { color: var(--accent); }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1>M3U Tuner Mode Setup</h1>
      <a href="/" class="btn" style="padding: 6px 12px; font-size: 0.8rem;">&larr; Back to Dashboard</a>
    </header>

    <div class="card">
      <h2 style="font-size: 1.2rem; margin-bottom: 10px;">Why Use M3U Tuner Mode?</h2>
      <p style="color: var(--text-muted); font-size: 0.9rem;">
        The HDHomeRun protocol does not support folder or category tags. By connecting Jellyfin to StreamTuner using its dedicated M3U playlist endpoint instead, Jellyfin will automatically group your channels into tabs (<strong>Australia</strong>, <strong>United States</strong>, <strong>Canada</strong>, <strong>Japan</strong>) for every user.
      </p>
    </div>

    <div class="card">
      <h2 style="font-size: 1.2rem; margin-bottom: 14px;">Your Generated StreamTuner Endpoints</h2>

      <div style="font-size: 0.85rem; color: var(--text-muted);">M3U Playlist URL (With Native Category Tags):</div>
      <div class="url-box">{{.BaseURL}}/channels.m3u</div>

      <div style="font-size: 0.85rem; color: var(--text-muted);">Aggregated XMLTV EPG URL:</div>
      <div class="url-box">{{.BaseURL}}/epg.xml</div>
    </div>

    <div class="card">
      <h2 style="font-size: 1.2rem; margin-bottom: 14px;">Setup Instructions for Jellyfin</h2>
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
        <strong>4. Set Simultaneous Stream Limit:</strong> Set the limit to <strong>4</strong> (or your preferred tuner count).
      </div>
      <div class="step">
        <strong>5. Connect Guide:</strong> Under <em>TV Guide Data Providers</em>, add an XMLTV provider with:
        <div style="font-family: monospace; color: var(--accent); margin-top: 4px;">{{.BaseURL}}/epg.xml</div>
      </div>
      <div class="step">
        <strong>6. Done!</strong> When users open Live TV in Jellyfin, they can filter by category tabs across the top screen.
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

			// Prepend country tag if not present
			if !strings.HasPrefix(currentName, "[") {
				currentName = fmt.Sprintf("%s %s", tag, currentName)
			}

			// Assign channel number according to block offset
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

	// Add custom channels
	customs := parseCustomChannels(appConfig.CustomChannels)
	for _, cust := range customs {
		replaced := false
		for i := range combined {
			if combined[i].GuideNumber == cust.GuideNumber || combined[i].URL == cust.URL {
				combined[i] = cust
				replaced = true
				break
			}
		}
		if !replaced {
			combined = append([]Channel{cust}, combined...)
		}
	}

	// Apply custom number overrides & enabled filter
	for i := range combined {
		if appConfig.CustomNumbers != nil {
			if overrideNo, exists := appConfig.CustomNumbers[combined[i].URL]; exists && overrideNo != "" {
				combined[i].GuideNumber = overrideNo
			}
		}

		nameMatch := matchesFilter(combined[i].GuideName, appConfig.FilterKeywords)
		if !nameMatch {
			combined[i].Enabled = false
		} else if appConfig.DisabledChannels != nil {
			if disabled := appConfig.DisabledChannels[combined[i].URL]; disabled {
				combined[i].Enabled = false
			}
		}
	}

	// Sort channels numerically by GuideNumber
	sort.Slice(combined, func(i, j int) bool {
		numI, errI := strconv.Atoi(combined[i].GuideNumber)
		numJ, errJ := strconv.Atoi(combined[j].GuideNumber)
		if errI == nil && errJ == nil {
			return numI < numJ
		}
		return combined[i].GuideNumber < combined[j].GuideNumber
	})

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
		DisabledChannels: make(map[string]bool),
		CustomNumbers:    make(map[string]string),
	}

	data, err := os.ReadFile(configPath)
	if err == nil {
		json.Unmarshal(data, &appConfig)
	}

	if appConfig.XMLTVURLs == "" && appConfig.XMLTVURL != "" {
		appConfig.XMLTVURLs = appConfig.XMLTVURL
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
			Config      Config
			Channels    []Channel
			ActiveCount int
		}{
			Config:      appConfig,
			Channels:    channels,
			ActiveCount: active,
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
				configLock.Lock()
				appConfig = newCfg
				loadAllChannels()
				cfgCopy := appConfig
				configLock.Unlock()
				saveConfig(cfgCopy)
				go refreshEPG()
			}
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	// Save Settings & Checkbox Toggles & Channel Number Edits
	http.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		r.ParseForm()

		tCount, _ := strconv.Atoi(r.FormValue("tuner_count"))
		if tCount < 1 {
			tCount = 1
		}

		activeList := r.Form["active_channels"]
		activeMap := make(map[string]bool)
		for _, u := range activeList {
			activeMap[u] = true
		}

		// Read editable channel numbers
		chURLs := r.Form["ch_url"]
		chNums := r.Form["ch_num"]
		newCustomNumbers := make(map[string]string)
		if appConfig.CustomNumbers != nil {
			for k, v := range appConfig.CustomNumbers {
				newCustomNumbers[k] = v
			}
		}
		if len(chURLs) == len(chNums) {
			for i := range chURLs {
				targetURL := chURLs[i]
				numVal := strings.TrimSpace(chNums[i])
				if numVal != "" {
					newCustomNumbers[targetURL] = numVal
				}
			}
		}

		configLock.Lock()
		newDisabled := make(map[string]bool)
		for i := range channels {
			u := channels[i].URL
			if activeMap[u] {
				channels[i].Enabled = true
			} else {
				channels[i].Enabled = false
				newDisabled[u] = true
			}
		}

		appConfig.FriendlyName = r.FormValue("friendly_name")
		appConfig.TunerCount = tCount
		appConfig.M3UURLs = strings.TrimSpace(r.FormValue("m3u_urls"))
		appConfig.CustomChannels = strings.TrimSpace(r.FormValue("custom_channels"))
		appConfig.XMLTVURLs = strings.TrimSpace(r.FormValue("xmltv_urls"))
		appConfig.FilterKeywords = strings.TrimSpace(r.FormValue("filter_keywords"))
		appConfig.DisabledChannels = newDisabled
		appConfig.CustomNumbers = newCustomNumbers

		loadAllChannels()
		cfgCopy := appConfig
		configLock.Unlock()

		saveConfig(cfgCopy)
		go refreshEPG()

		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	// M3U Playlist Endpoint with native group-title tags
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
			FriendlyName:    appConfig.FriendlyName,
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

	log.Printf("StreamTuner running on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %s\n", err)
	}
}
