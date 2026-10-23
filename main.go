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
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	FriendlyName     string          `json:"friendly_name"`
	TunerCount       int             `json:"tuner_count"`
	M3UURLs          string          `json:"m3u_urls"`
	XMLTVURLs        string          `json:"xmltv_urls"`
	XMLTVURL         string          `json:"xmltv_url,omitempty"`
	FilterKeywords   string          `json:"filter_keywords"`
	DisabledChannels map[string]bool `json:"disabled_channels"`
}

type Channel struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	Logo        string `json:"Logo,omitempty"`
	TvgID       string `json:"TvgID,omitempty"`
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
    header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; padding-bottom: 14px; border-bottom: 1px solid var(--border); }
    .logo-group { display: flex; align-items: center; gap: 10px; }
    .logo-group h1 { font-size: 1.4rem; color: var(--accent); }
    .badge { background: var(--border); color: var(--accent); padding: 4px 10px; border-radius: 999px; font-size: 0.75rem; font-weight: 600; }
    .header-links { display: flex; gap: 14px; }
    .header-links a { color: var(--accent); font-size: 0.85rem; text-decoration: none; font-weight: 500; }
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
        <span class="badge">v0.5.0</span>
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
          <label>M3U / M3U8 Playlist URLs (One URL per line)</label>
          <textarea name="m3u_urls" placeholder="https://example.com/playlist1.m3u8&#10;https://example.com/playlist2.m3u8">{{.Config.M3UURLs}}</textarea>
        </div>
        <div class="form-group">
          <label>XMLTV EPG URLs (One URL per line — supports .xml and .xml.gz)</label>
          <textarea name="xmltv_urls" placeholder="https://example.com/epg1.xml&#10;https://example.com/epg2.xml.gz">{{.Config.XMLTVURLs}}</textarea>
        </div>
        <div class="form-group">
          <label>Channel Name Filter (Optional comma-separated keywords)</label>
          <input type="text" name="filter_keywords" value="{{.Config.FilterKeywords}}" placeholder="e.g.: ABC, Seven, Nine, 10, SBS, BBC">
        </div>
        <button type="submit" class="btn">Save &amp; Refresh Feeds</button>
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
              <th>EPG ID</th>
              <th>Stream Link</th>
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
              <td style="color: var(--text-muted); font-size: 0.8rem;">{{if .TvgID}}{{.TvgID}}{{else}}-{{end}}</td>
              <td class="channel-url"><a href="/stream/{{.GuideNumber}}" target="_blank" style="color: var(--accent); text-decoration: none;">/stream/{{.GuideNumber}}</a></td>
            </tr>
            {{end}}
          </tbody>
        </table>
        {{else}}
          <p style="color: var(--text-muted); font-size: 0.9rem;">No channels loaded. Enter your playlist URLs above and click Save.</p>
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
	var currentName, currentLogo, currentTvgID string
	chNum := startChNum

	reTvgID := regexp.MustCompile(`tvg-id="([^"]+)"`)
	reTvgName := regexp.MustCompile(`tvg-name="([^"]+)"`)
	reTvgLogo := regexp.MustCompile(`tvg-logo="([^"]+)"`)
	reTvgChNo := regexp.MustCompile(`tvg-chno="([^"]+)"`)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXTINF:") {
			currentName = ""
			currentLogo = ""
			currentTvgID = ""

			if m := reTvgID.FindStringSubmatch(line); len(m) > 1 {
				currentTvgID = m[1]
			}
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
				TvgID:       currentTvgID,
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
		XMLTVURLs:        "",
		FilterKeywords:   "",
		DisabledChannels: make(map[string]bool),
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

	// Periodic 12-hour EPG refresh
	go func() {
		time.Sleep(5 * time.Second)
		refreshEPG()
		ticker := time.NewTicker(12 * time.Hour)
		for range ticker.C {
			refreshEPG()
		}
	}()

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
		appConfig.XMLTVURLs = strings.TrimSpace(r.FormValue("xmltv_urls"))
		appConfig.FilterKeywords = strings.TrimSpace(r.FormValue("filter_keywords"))
		appConfig.DisabledChannels = newDisabled

		loadAllChannels()
		cfgCopy := appConfig
		configLock.Unlock()

		saveConfig(cfgCopy)
		go refreshEPG()

		http.Redirect(w, r, "
