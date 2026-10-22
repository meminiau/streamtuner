package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type Config struct {
	FriendlyName     string          `json:"friendly_name"`
	TunerCount       int             `json:"tuner_count"`
	M3UURLs          string          `json:"m3u_urls"`
	XMLTVURL         string          `json:"xmltv_url"`
	FilterKeywords   string          `json:"filter_keywords"`
	DisabledChannels map[string]bool `json:"disabled_channels"`
}

type Channel struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	Logo        string `json:"Logo,omitempty"`
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
	configLock sync.RWMutex
	appConfig  Config
	channels   []Channel
	configPath string
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
      --danger: #ef4444;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    body { background: var(--bg); color: var(--text); padding: 16px; display: flex; justify-content: center; }
    .container { width: 100%; max-width: 960px; }
    header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; padding-bottom: 14px; border-bottom: 1px solid var(--border); }
    .logo-group { display: flex; align-items: center; gap: 10px; }
    .logo-group h1 { font-size: 1.4rem; color: var(--accent); }
    .badge { background: var(--border); color: var(--accent); padding: 4px 10px; border-radius: 999px; font-size: 0.75rem; font-weight: 600; }
    .card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 20px; margin-bottom: 20px; box-shadow: 0 4px 20px rgba(0,0,0,0.3); }
    .card h2 { font-size: 1.15rem; margin-bottom: 14px; color: var(--text); }
    .form-group { margin-bottom: 14px; }
    label { display: block; margin-bottom: 6px; font-size: 0.85rem; color: var(--text-muted); font-weight: 500; }
    input[type="text"], input[type="number"], textarea { width: 100%; padding: 10px 12px; background: #0c101a; border: 1px solid var(--border); border-radius: 8px; color: var(--text); font-size: 0.95rem; }
    textarea { resize: vertical; min-height: 80px; font-family: monospace; font-size: 0.85rem; }
    input:focus, textarea:focus { outline: none; border-color: var(--accent); }
    .btn { background: var(--accent); color: #051015; border: none; padding: 10px 18px; font-size: 0.9rem; font-weight: 700; border-radius: 8px; cursor: pointer; transition: background 0.2s; }
    .btn:hover { background: var(--accent-hover); }
    .btn-secondary { background: var(--border); color: var(--text); font-weight: 600; padding: 6px 12px; font-size: 0.8rem; }
    .stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); gap: 12px; margin-bottom: 20px; }
    .stat-box { background: var(--card); border: 1px solid var(--border); border-radius: 10px; padding: 14px; text-align: center; }
    .stat-box .label { font-size: 0.8rem; color: var(--text-muted); }
    .stat-box .val { font-size: 1.6rem; font-weight: 700; color: var(--accent); margin-top: 4px; }
    .table-actions { display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px; flex-wrap: wrap; gap: 8px; }
    .channel-table { width: 100%; border-collapse: collapse; }
    .channel-table th, .channel-table td { padding: 10px 8px; text-align: left; border-bottom: 1px solid var(--border); font-size: 0.85rem; }
    .channel-table th { color: var(--text-muted); font-weight: 600; }
    .channel-url { max-width: 250px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    input[type="checkbox"] { width: 18px; height: 18px; accent-color: var(--accent); cursor: pointer; }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <div class="logo-group">
        <h1>StreamTuner</h1>
        <span class="badge">v0.4.0</span>
      </div>
      <div>
        <a href="/lineup.json" target="_blank" style="color: var(--accent); font-size: 0.85rem; text-decoration: none;">View Active lineup.json</a>
      </div>
    </header>

    <div class="stats">
      <div class="stat-box">
        <div class="label">Tuners</div>
        <div class="val">{{.Config.TunerCount}}</div>
      </div>
      <div class="stat-box">
        <div class="label">Total Found</div>
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
          <label>M3U / M3U8 Playlist URLs (Paste multiple URLs on separate lines for international feeds)</label>
          <textarea name="m3u_urls" placeholder="https://example.com/playlist1.m3u8&#10;https://example.com/international.m3u8">{{.Config.M3UURLs}}</textarea>
        </div>
        <div class="form-group">
          <label>XMLTV EPG URL</label>
          <input type="text" name="xmltv_url" value="{{.Config.XMLTVURL}}" placeholder="https://example.com/epg.xml">
        </div>
        <div class="form-group">
          <label>Channel Name Filter (Optional comma-separated keywords, e.g.: ABC, Seven, Nine, 10, SBS, BBC)</label>
          <input type="text" name="filter_keywords" value="{{.Config.FilterKeywords}}" placeholder="Leave blank to include all channels">
        </div>
        <button type="submit" class="btn">Save &amp; Reload Feeds</button>
      </div>

      <div class="card">
        <div class="table-actions">
          <h2>Channel Lineup ({{len .Channels}})</h2>
          <div>
            <button type="button" class="btn btn-secondary" onclick="toggleAll(true)">Select All</button>
            <button type="button" class="btn btn-secondary" onclick="toggleAll(false)">Deselect All</button>
            <button type="submit" class="btn" style="padding: 6px 14px; font-size: 0.8rem;">Apply Changes</button>
          </div>
        </div>

        {{if .Channels}}
        <table class="channel-table">
          <thead>
            <tr>
              <th style="width: 40px; text-align: center;">Active</th>
              <th style="width: 60px;">#</th>
              <th>Channel Name</th>
              <th>Test Link</th>
            </tr>
          </thead>
          <tbody>
            {{range .Channels}}
            <tr>
              <td style="text-align: center;">
                <input type="checkbox" name="active_channels" value="{{.GuideNumber}}" {{if .Enabled}}checked{{end}}>
              </td>
              <td><strong>{{.GuideNumber}}</strong></td>
              <td>{{.GuideName}}</td>
              <td class="channel-url"><a href="/stream/{{.GuideNumber}}" target="_blank" style="color: var(--accent); text-decoration: none;">/stream/{{.GuideNumber}}</a></td>
            </tr>
            {{end}}
          </tbody>
        </table>
        {{else}}
          <p style="color: var(--text-muted); font-size: 0.9rem;">No channels loaded. Enter one or more M3U playlist URLs above and click Save.</p>
        {{end}}
      </div>
    </form>
  </div>

  <script>
    function toggleAll(status) {
      const boxes = document.querySelectorAll('input[name="active_channels"]');
      boxes.forEach(cb => cb.checked = status);
    }
  </script>
</body>
</html>`

func fetchAndParseM3U(url string, startChNum int) ([]Channel, int, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, startChNum, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, startChNum, err
	}
	defer resp.Body.Close()

	var parsed []Channel
	scanner := bufio.NewScanner(resp.Body)
	var currentName, currentLogo string
	chNum := startChNum

	reTvgName := regexp.MustCompile(`tvg-name="([^"]+)"`)
	reTvgLogo := regexp.MustCompile(`tvg-logo="([^"]+)"`)
	reTvgChNo := regexp.MustCompile(`tvg-chno="([^"]+)"`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXTINF:") {
			currentName = ""
			currentLogo = ""

			if m := reTvgName.FindStringSubmatch(line); len(m) > 1 {
				currentName = m[1]
			}
			if m := reTvgLogo.FindStringSubmatch(line); len(m) > 1 {
				currentLogo = m[1]
			}
			if currentName == "" {
				parts := strings.Split(line, ",")
				if len(parts) > 1 {
					currentName = strings.TrimSpace(parts[len(parts)-1])
				}
			}

			guideNo := fmt.Sprintf("%d", chNum)
			if m := reTvgChNo.FindStringSubmatch(line); len(m) > 1 {
				guideNo = m[1]
			}

			parsed = append(parsed, Channel{
				GuideNumber: guideNo,
				GuideName:   currentName,
				Logo:        currentLogo,
				Enabled:     true,
			})
			chNum++
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
	return cleaned, chNum, nil
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
	if appConfig.M3UURLs == "" {
		channels = nil
		return
	}

	urls := strings.Split(appConfig.M3UURLs, "\n")
	var combined []Channel
	nextCh := 1

	for _, u := range urls {
		trimmed := strings.TrimSpace(u)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		chList, next, err := fetchAndParseM3U(trimmed, nextCh)
		if err != nil {
			log.Printf("Error fetching playlist %s: %v", trimmed, err)
			continue
		}
		combined = append(combined, chList...)
		nextCh = next
	}

	// Apply keyword filtering and disabled state
	for i := range combined {
		nameMatch := matchesFilter(combined[i].GuideName, appConfig.FilterKeywords)
		if !nameMatch {
			combined[i].Enabled = false
		} else if appConfig.DisabledChannels != nil {
			if disabled := appConfig.DisabledChannels[combined[i].GuideNumber]; disabled {
				combined[i].Enabled = false
			}
		}
	}

	channels = combined
}

func loadConfig() {
	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" {
		configDir = "/config"
	}
	os.MkdirAll(configDir, 0755)
	configPath = filepath.Join(configDir, "settings.json")

	appConfig = Config{
		FriendlyName:     "StreamTuner",
		TunerCount:       4,
		M3UURLs:          "",
		XMLTVURL:         "",
		FilterKeywords:   "",
		DisabledChannels: make(map[string]bool),
	}

	data, err := os.ReadFile(configPath)
	if err == nil {
		json.Unmarshal(data, &appConfig)
	}

	if appConfig.DisabledChannels == nil {
		appConfig.DisabledChannels = make(map[string]bool)
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

	// Web Dashboard
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

	// Save Settings & Checkbox Toggles
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
		for _, chNo := range activeList {
			activeMap[chNo] = true
		}

		configLock.Lock()
		newDisabled := make(map[string]bool)
		for i := range channels {
			chNo := channels[i].GuideNumber
			if activeMap[chNo] {
				channels[i].Enabled = true
			} else {
				channels[i].Enabled = false
				newDisabled[chNo] = true
			}
		}

		appConfig.FriendlyName = r.FormValue("friendly_name")
		appConfig.TunerCount = tCount
		appConfig.M3UURLs = strings.TrimSpace(r.FormValue("m3u_urls"))
		appConfig.XMLTVURL = strings.TrimSpace(r.FormValue("xmltv_url"))
		appConfig.FilterKeywords = strings.TrimSpace(r.FormValue("filter_keywords"))
		appConfig.DisabledChannels = newDisabled

		loadAllChannels()
		cfgCopy := appConfig
		configLock.Unlock()

		saveConfig(cfgCopy)
		http.Redirect(w, r, "/", http.StatusSeeOther)
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

	// HDHomeRun Lineup (Serves ONLY Enabled channels)
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

	// Live Stream Proxy
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
