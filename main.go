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
	FriendlyName string `json:"friendly_name"`
	TunerCount   int    `json:"tuner_count"`
	M3UURL       string `json:"m3u_url"`
	XMLTVURL     string `json:"xmltv_url"`
}

type Channel struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	Logo        string `json:"Logo,omitempty"`
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
    }
    * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    body { background: var(--bg); color: var(--text); padding: 20px; display: flex; justify-content: center; }
    .container { width: 100%; max-width: 900px; }
    header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 24px; padding-bottom: 16px; border-bottom: 1px solid var(--border); }
    .logo-group { display: flex; align-items: center; gap: 12px; }
    .logo-group h1 { font-size: 1.5rem; color: var(--accent); }
    .badge { background: var(--border); color: var(--accent); padding: 4px 10px; border-radius: 999px; font-size: 0.8rem; font-weight: 600; }
    .card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 24px; margin-bottom: 24px; box-shadow: 0 4px 20px rgba(0,0,0,0.3); }
    .card h2 { font-size: 1.2rem; margin-bottom: 16px; color: var(--text); }
    .form-group { margin-bottom: 16px; }
    label { display: block; margin-bottom: 6px; font-size: 0.9rem; color: var(--text-muted); font-weight: 500; }
    input[type="text"], input[type="number"] { width: 100%; padding: 12px 14px; background: #0c101a; border: 1px solid var(--border); border-radius: 8px; color: var(--text); font-size: 1rem; }
    input[type="text"]:focus, input[type="number"]:focus { outline: none; border-color: var(--accent); }
    button { background: var(--accent); color: #051015; border: none; padding: 12px 24px; font-size: 1rem; font-weight: 700; border-radius: 8px; cursor: pointer; transition: background 0.2s; }
    button:hover { background: var(--accent-hover); }
    .stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 16px; margin-bottom: 24px; }
    .stat-box { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 16px; text-align: center; }
    .stat-box .val { font-size: 1.8rem; font-weight: 700; color: var(--accent); margin-top: 4px; }
    .channel-table { width: 100%; border-collapse: collapse; margin-top: 12px; }
    .channel-table th, .channel-table td { padding: 12px; text-align: left; border-bottom: 1px solid var(--border); font-size: 0.9rem; }
    .channel-table th { color: var(--text-muted); font-weight: 600; }
    .channel-url { max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-muted); }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <div class="logo-group">
        <h1>StreamTuner</h1>
        <span class="badge">v0.3.0</span>
      </div>
      <div>
        <a href="/discover.json" target="_blank" style="color: var(--accent); font-size: 0.9rem; text-decoration: none;">View discover.json</a>
      </div>
    </header>

    <div class="stats">
      <div class="stat-box">
        <div>Active Tuners</div>
        <div class="val">{{.Config.TunerCount}}</div>
      </div>
      <div class="stat-box">
        <div>Channels Loaded</div>
        <div class="val">{{len .Channels}}</div>
      </div>
    </div>

    <div class="card">
      <h2>Tuner Configuration</h2>
      <form action="/save" method="POST">
        <div class="form-group">
          <label>Device Friendly Name</label>
          <input type="text" name="friendly_name" value="{{.Config.FriendlyName}}" required>
        </div>
        <div class="form-group">
          <label>Number of Virtual Tuners</label>
          <input type="number" name="tuner_count" value="{{.Config.TunerCount}}" min="1" max="32" required>
        </div>
        <div class="form-group">
          <label>M3U / M3U8 Playlist URL</label>
          <input type="text" name="m3u_url" value="{{.Config.M3UURL}}" placeholder="https://example.com/playlist.m3u8">
        </div>
        <div class="form-group">
          <label>XMLTV EPG URL</label>
          <input type="text" name="xmltv_url" value="{{.Config.XMLTVURL}}" placeholder="https://example.com/epg.xml">
        </div>
        <button type="submit">Save &amp; Reload Channels</button>
      </form>
    </div>

    <div class="card">
      <h2>Channel Lineup ({{len .Channels}})</h2>
      {{if .Channels}}
      <table class="channel-table">
        <thead>
          <tr>
            <th>#</th>
            <th>Channel Name</th>
            <th>Source Stream</th>
          </tr>
        </thead>
        <tbody>
          {{range .Channels}}
          <tr>
            <td><strong>{{.GuideNumber}}</strong></td>
            <td>{{.GuideName}}</td>
            <td class="channel-url">{{.URL}}</td>
          </tr>
          {{end}}
        </tbody>
      </table>
      {{else}}
        <p style="color: var(--text-muted); font-size: 0.95rem;">No channels loaded. Enter an M3U playlist URL above and click Save.</p>
      {{end}}
    </div>
  </div>
</body>
</html>`

func parseM3U(url string) ([]Channel, error) {
	if url == "" {
		return nil, nil
	}

	client := &http.Client{}
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
	var currentName, currentLogo string
	chNum := 1

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
	return cleaned, nil
}

func loadConfig() {
	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" {
		configDir = "/config"
	}
	os.MkdirAll(configDir, 0755)
	configPath = filepath.Join(configDir, "settings.json")

	appConfig = Config{
		FriendlyName: "StreamTuner",
		TunerCount:   4,
		M3UURL:       "",
		XMLTVURL:     "",
	}

	data, err := os.ReadFile(configPath)
	if err == nil {
		json.Unmarshal(data, &appConfig)
	}

	if appConfig.M3UURL != "" {
		var err error
		channels, err = parseM3U(appConfig.M3UURL)
		if err != nil {
			log.Printf("Failed to load initial playlist: %v", err)
		}
	}
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
		data := struct {
			Config   Config
			Channels []Channel
		}{
			Config:   appConfig,
			Channels: channels,
		}
		configLock.RUnlock()
		tmpl.Execute(w, data)
	})

	// Save Settings
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

		newCfg := Config{
			FriendlyName: r.FormValue("friendly_name"),
			TunerCount:   tCount,
			M3UURL:       strings.TrimSpace(r.FormValue("m3u_url")),
			XMLTVURL:     strings.TrimSpace(r.FormValue("xmltv_url")),
		}

		newChannels, err := parseM3U(newCfg.M3UURL)
		if err != nil {
			log.Printf("M3U fetch error: %v", err)
		}

		configLock.Lock()
		appConfig = newCfg
		channels = newChannels
		configLock.Unlock()

		saveConfig(newCfg)
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

	// HDHomeRun Lineup pointing to internal proxy
	http.HandleFunc("/lineup.json", func(w http.ResponseWriter, r *http.Request) {
		configLock.RLock()
		defer configLock.RUnlock()

		baseURL := getBaseURL(r)
		var lineup []LineupItem
		for _, c := range channels {
			lineup = append(lineup, LineupItem{
				GuideNumber: c.GuideNumber,
				GuideName:   c.GuideName,
				URL:         fmt.Sprintf("%s/stream/%s", baseURL, c.GuideNumber),
			})
		}

		w.Header().Set("Content-Type", "application/json")
		if len(lineup) == 0 {
			json.NewEncoder(w).Encode([]LineupItem{
				{GuideNumber: "1", GuideName: "StreamTuner Test", URL: fmt.Sprintf("%s/stream/1", baseURL)},
			})
			return
		}
		json.NewEncoder(w).Encode(lineup)
	})

	// Live Stream Proxy (Remuxes HLS to MPEG-TS with auto-reconnect)
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
			"-reconnect", "1",
			"-reconnect_at_eof", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "5",
			"-user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
			"-i", targetURL,
			"-c", "copy",
			"-f", "mpegts",
			"pipe:1",
		)

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

		// Stream bytes directly to client
		io.Copy(w, stdout)
		cmd.Process.Kill()
		cmd.Wait()
	})

	log.Printf("StreamTuner running on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %s\n", err)
	}
}
