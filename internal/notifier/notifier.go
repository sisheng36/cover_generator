package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"embytool/internal/config"
	"embytool/internal/emby"
)

var (
	tmdbIDPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)[\[{](?:tmdbid|tmdb)[=:-](\d+)[\]}]`),
		regexp.MustCompile(`(?i)tmdb[=:-](\d+)`),
		regexp.MustCompile(`(?i)tmdbid[=:-](\d+)`),
	}
	webhookActions = map[string]string{
		"library.new":             "新入库",
		"ItemAdded":               "新入库",
		"system.notificationtest": "测试",
		"playback.start":          "开始播放",
		"playback.stop":           "停止播放",
		"user.authenticated":      "登录成功",
		"user.authenticationfailed": "登录失败",
		"media.play":              "开始播放",
		"media.stop":              "停止播放",
		"PlaybackStart":           "开始播放",
		"PlaybackStop":            "停止播放",
		"item.rate":               "标记了",
	}
	mediaIcons = map[string]string{
		"MOV": "🎬",
		"TV":  "📺",
		"AUD": "🎧",
		"BOX": "📦",
	}
)

type Event struct {
	Event          string
	ServerName     string
	Channel        string
	ItemID         string
	ItemType       string
	ItemName       string
	SeriesName     string
	SeriesID       string
	SeasonID       *int
	EpisodeID      *int
	TmdbID         string
	ProductionYear any
	Overview       string
	ImageURL       string
	ItemPath       string
	JSONObject     map[string]any
}

type Service struct {
	mu               sync.Mutex
	pendingMessages  map[string][]Event
	aggregateTimers  map[string]*time.Timer
	dedupeCache      map[string]time.Time
}

const (
	aggregateDedupExpiration = 30
)

func NewService() *Service {
	return &Service{
		pendingMessages: map[string][]Event{},
		aggregateTimers: map[string]*time.Timer{},
		dedupeCache:     map[string]time.Time{},
	}
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func (s *Service) Stats() (dedupeSize int, pendingKeys int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.dedupeCache), len(s.pendingMessages)
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, timer := range s.aggregateTimers {
		timer.Stop()
	}
	s.aggregateTimers = map[string]*time.Timer{}
	s.pendingMessages = map[string][]Event{}
}

func extractTMDBID(item map[string]any) string {
	path := asString(item["Path"])
	for _, pattern := range tmdbIDPatterns {
		if matches := pattern.FindStringSubmatch(path); len(matches) > 1 {
			return matches[1]
		}
	}
	if providerIDs, ok := item["ProviderIds"].(map[string]any); ok {
		if id := strings.TrimSpace(asString(providerIDs["Tmdb"])); id != "" {
			return id
		}
	}
	return ""
}

func extractYearFromValue(value any) string {
	switch t := value.(type) {
	case nil:
		return ""
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.Itoa(int(t))
	case json.Number:
		match := regexp.MustCompile(`\b(\d{4})\b`).FindStringSubmatch(t.String())
		if len(match) > 1 {
			return match[1]
		}
		if i, err := t.Int64(); err == nil {
			return strconv.FormatInt(i, 10)
		}
		return ""
	case string:
		match := regexp.MustCompile(`\b(\d{4})\b`).FindStringSubmatch(t)
		if len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

func eventItem(event Event) map[string]any {
	if event.JSONObject == nil {
		return map[string]any{}
	}
	if item, ok := event.JSONObject["Item"].(map[string]any); ok {
		return item
	}
	return map[string]any{}
}

func resolveDisplayName(event Event) string {
	item := eventItem(event)
	if event.ItemType == "TV" {
		if v := strings.TrimSpace(event.SeriesName); v != "" {
			return v
		}
		if v := strings.TrimSpace(asString(item["SeriesName"])); v != "" {
			return v
		}
		if v := strings.TrimSpace(event.ItemName); v != "" {
			return v
		}
		if v := strings.TrimSpace(asString(item["Name"])); v != "" {
			return v
		}
		return ""
	}
	if v := strings.TrimSpace(event.ItemName); v != "" {
		return v
	}
	if v := strings.TrimSpace(asString(item["Name"])); v != "" {
		return v
	}
	return ""
}

func resolveYear(event Event, tmdbInfo map[string]any) string {
	item := eventItem(event)
	candidates := []any{
		event.ProductionYear,
		item["ProductionYear"],
	}
	if tmdbInfo != nil {
		candidates = append(candidates, tmdbInfo["first_air_date"], tmdbInfo["release_date"])
	}
	for _, candidate := range candidates {
		if year := extractYearFromValue(candidate); year != "" {
			return year
		}
	}
	return ""
}

func buildLibraryTitle(name, itemType, year string) string {
	icon := mediaIcons[itemType]
	if icon == "" {
		icon = "🎬"
	}
	title := name
	if title == "" {
		title = "未知条目"
	}
	if year != "" {
		title = fmt.Sprintf("%s (%s)", title, year)
	}
	return fmt.Sprintf("%s %s ✨ 入库成功", icon, title)
}

func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return strings.TrimSpace(string(runes[:limit-3])) + "..."
}

func formatTelegramPayload(title, text string, limit *int) string {
	safeTitle := "<b>" + html.EscapeString(title) + "</b>"
	if strings.TrimSpace(text) == "" {
		if limit == nil {
			return safeTitle
		}
		return truncateText(safeTitle, *limit)
	}

	rawText := strings.TrimSpace(text)
	if limit != nil {
		allowed := *limit - utf8.RuneCountInString(safeTitle) - 2
		if allowed < 0 {
			allowed = 0
		}
		rawText = truncateText(rawText, allowed)
	}

	safeText := html.EscapeString(rawText)
	if safeText == "" {
		return safeTitle
	}
	return safeTitle + "\n\n" + safeText
}

func resolveTMDBImage(tmdbInfo map[string]any, preferBackdrop bool) string {
	if tmdbInfo == nil {
		return ""
	}
	var path string
	if preferBackdrop {
		path, _ = tmdbInfo["backdrop_path"].(string)
		if path == "" {
			path, _ = tmdbInfo["poster_path"].(string)
		}
	} else {
		path, _ = tmdbInfo["poster_path"].(string)
		if path == "" {
			path, _ = tmdbInfo["backdrop_path"].(string)
		}
	}
	if path == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/original" + path
}

func formatProgress(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	if value == "" {
		return ""
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return value
	}
	formatted := fmt.Sprintf("%.2f", parsed)
	formatted = strings.TrimRight(formatted, "0")
	formatted = strings.TrimRight(formatted, ".")
	if !strings.Contains(formatted, ".") {
		formatted += ".0"
	}
	return formatted
}

func appendTimeIfNeeded(lines []string) []string {
	for _, line := range lines {
		if strings.HasPrefix(line, "⏰ 时间:") {
			return lines
		}
	}
	insertAt := 0
	if len(lines) > 0 && strings.HasPrefix(lines[0], "🎞️ ") {
		insertAt = 1
	}
	nowLine := "⏰ 时间: " + time.Now().Format("2006-01-02 15:04:05")
	if insertAt >= len(lines) {
		return append(lines, nowLine)
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, nowLine)
	out = append(out, lines[insertAt:]...)
	return out
}

func resolveEpisodeLine(events []Event) string {
	episodesDetail := mergeContinuousEpisodes(events)
	if episodesDetail != "" {
		return "🎞️ 集数: " + episodesDetail
	}
	if len(events) > 1 {
		return fmt.Sprintf("🎞️ 集数: 共%d个文件", len(events))
	}
	first := Event{}
	if len(events) > 0 {
		first = events[0]
	}
	if first.SeasonID != nil && first.EpisodeID != nil {
		return fmt.Sprintf("🎞️ 集数: S%02dE%02d", *first.SeasonID, *first.EpisodeID)
	}
	return "🎞️ 集数: 未知"
}

func resolveOverviewLine(overview string) string {
	text := truncateText(strings.TrimSpace(overview), 240)
	if text == "" {
		text = "暂无剧情"
	}
	return "📝 剧情: " + text
}

func resolveTMDBRatingLine(tmdbInfo map[string]any) string {
	if tmdbInfo == nil {
		return ""
	}
	score, ok := tmdbInfo["vote_average"].(float64)
	if !ok {
		if scoreStr, ok := tmdbInfo["vote_average"].(string); ok {
			if parsed, err := strconv.ParseFloat(scoreStr, 64); err == nil {
				score = parsed
				ok = true
			}
		}
	}
	if !ok || score <= 0 {
		return ""
	}
	ratingLine := fmt.Sprintf("⭐ TMDB评分: %.1f/10", score)
	votes := 0
	switch v := tmdbInfo["vote_count"].(type) {
	case float64:
		votes = int(v)
	case int:
		votes = v
	case int64:
		votes = int(v)
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			votes = parsed
		}
	}
	if votes > 0 {
		ratingLine += fmt.Sprintf(" (%d人评分)", votes)
	}
	return ratingLine
}

func parseEmbyWebhook(data map[string]any) *Event {
	event := asString(data["Event"])
	if event == "" {
		event = asString(data["event"])
	}
	if event == "" {
		return nil
	}

	item, _ := data["Item"].(map[string]any)
	server, _ := data["Server"].(map[string]any)
	eventAction := event
	if event == "ItemAdded" {
		eventAction = "library.new"
	}

	itemTypeRaw := strings.ToUpper(asString(item["Type"]))
	mediaTypeMap := map[string]string{
		"MOVIE": "MOV",
		"MOV":   "MOV",
		"EPISODE": "TV",
		"TV":       "TV",
		"SHOW":     "TV",
		"MUSIC":    "AUD",
		"AUDIO":    "AUD",
		"AUDIOBOOK": "AUD",
		"BOXSET":    "BOX",
	}
	itemType := mediaTypeMap[itemTypeRaw]
	if itemType == "" {
		itemType = "MOV"
	}
	if itemType != "TV" && asString(item["SeriesId"]) != "" {
		itemType = "TV"
	}

	var seasonID *int
	if v := intValue(item["ParentIndexNumber"]); v != nil {
		seasonID = v
	}
	var episodeID *int
	if v := intValue(item["IndexNumber"]); v != nil {
		episodeID = v
	}

	seriesID := asString(item["SeriesId"])
	if seriesID == "" {
		seriesID = asString(item["SeriesName"])
	}

	return &Event{
		Event:          eventAction,
		ServerName:     asString(server["Name"]),
		Channel:        "emby",
		ItemID:         asString(item["Id"]),
		ItemType:       itemType,
		ItemName:       asString(item["Name"]),
		SeriesName:     asString(item["SeriesName"]),
		SeriesID:       seriesID,
		SeasonID:       seasonID,
		EpisodeID:      episodeID,
		TmdbID:         extractTMDBID(item),
		ProductionYear: item["ProductionYear"],
		Overview:       asString(item["Overview"]),
		ImageURL:       "",
		ItemPath:       asString(item["Path"]),
		JSONObject:     data,
	}
}

func intValue(v any) *int {
	switch t := v.(type) {
	case int:
		return &t
	case int64:
		vv := int(t)
		return &vv
	case float64:
		vv := int(t)
		return &vv
	case json.Number:
		if parsed, err := t.Int64(); err == nil {
			vv := int(parsed)
			return &vv
		}
		if parsed, err := t.Float64(); err == nil {
			vv := int(parsed)
			return &vv
		}
		return nil
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return &parsed
		}
	}
	return nil
}

func fetchTMDBInfo(tmdbAPIKey, tmdbID, mediaType string) map[string]any {
	if tmdbID == "" || tmdbAPIKey == "" {
		return map[string]any{}
	}
	media := "tv"
	if mediaType == "movie" {
		media = "movie"
	}
	base := fmt.Sprintf("https://api.themoviedb.org/3/%s/%s", media, tmdbID)
	client := &http.Client{Timeout: 10 * time.Second}
	for _, lang := range []string{"zh-CN", "en-US"} {
		reqURL := base + "?api_key=" + url.QueryEscape(tmdbAPIKey) + "&language=" + url.QueryEscape(lang)
		req, err := http.NewRequest(http.MethodGet, reqURL, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err == nil {
			return out
		}
	}
	return map[string]any{}
}

func mergeContinuousEpisodes(events []Event) string {
	seasonEpisodes := map[int][]int{}
	for _, ev := range events {
		if ev.SeasonID != nil && ev.EpisodeID != nil {
			seasonEpisodes[*ev.SeasonID] = append(seasonEpisodes[*ev.SeasonID], *ev.EpisodeID)
		}
	}
	merged := make([]string, 0)
	seasonKeys := make([]int, 0, len(seasonEpisodes))
	for season := range seasonEpisodes {
		seasonKeys = append(seasonKeys, season)
	}
	sort.Ints(seasonKeys)
	for _, season := range seasonKeys {
		episodes := seasonEpisodes[season]
		sort.Ints(episodes)
		episodes = uniqueInts(episodes)
		if len(episodes) == 0 {
			continue
		}
		start := episodes[0]
		end := episodes[0]
		for i := 1; i < len(episodes); i++ {
			if episodes[i] == end+1 {
				end = episodes[i]
				continue
			}
			merged = append(merged, episodeRangeString(season, start, end))
			start = episodes[i]
			end = episodes[i]
		}
		merged = append(merged, episodeRangeString(season, start, end))
	}
	return strings.Join(merged, " ")
}

func uniqueInts(values []int) []int {
	if len(values) == 0 {
		return values
	}
	out := []int{values[0]}
	last := values[0]
	for _, v := range values[1:] {
		if v == last {
			continue
		}
		out = append(out, v)
		last = v
	}
	return out
}

func episodeRangeString(season, start, end int) string {
	if start == end {
		return fmt.Sprintf("S%02dE%02d", season, start)
	}
	return fmt.Sprintf("S%02dE%02d-S%02dE%02d", season, start, season, end)
}

func buildTVMessage(events []Event, tmdbAPIKey string) (string, string, string) {
	if len(events) == 0 {
		return "", "", ""
	}
	first := events[0]
	showName := resolveDisplayName(first)
	for _, ev := range events {
		if sn := strings.TrimSpace(ev.SeriesName); sn != "" {
			showName = sn
			break
		}
		if jo, ok := ev.JSONObject["Item"].(map[string]any); ok {
			if sn := strings.TrimSpace(asString(jo["SeriesName"])); sn != "" {
				showName = sn
				break
			}
		}
	}
	tmdbInfo := map[string]any{}
	if first.TmdbID != "" {
		tmdbInfo = fetchTMDBInfo(tmdbAPIKey, first.TmdbID, "tv")
	}
	title := buildLibraryTitle(showName, "TV", resolveYear(first, tmdbInfo))
	texts := []string{resolveEpisodeLine(events)}
	if ratingLine := resolveTMDBRatingLine(tmdbInfo); ratingLine != "" {
		texts = append(texts, ratingLine)
	}
	overview := strings.TrimSpace(asString(tmdbInfo["overview"]))
	texts = append(texts, resolveOverviewLine(overview))
	texts = appendTimeIfNeeded(texts)
	imageURL := resolveTMDBImage(tmdbInfo, true)
	return title, strings.Join(texts, "\n"), imageURL
}

func buildGenericMessage(event Event, tmdbAPIKey string) (string, string, string) {
	eventAction := event.Event
	actionText := webhookActions[eventAction]
	if actionText == "" {
		actionText = eventAction
	}
	itemType := event.ItemType
	displayName := resolveDisplayName(event)
	tmdbInfo := map[string]any{}
	if eventAction == "library.new" && event.TmdbID != "" && (itemType == "MOV" || itemType == "TV") {
		mediaType := "tv"
		if itemType == "MOV" {
			mediaType = "movie"
		}
		tmdbInfo = fetchTMDBInfo(tmdbAPIKey, event.TmdbID, mediaType)
	}

	var title string
	if eventAction == "library.new" {
		title = buildLibraryTitle(displayName, itemType, resolveYear(event, tmdbInfo))
	} else {
		typeMap := map[string]string{"MOV": "电影", "TV": "剧集", "AUD": "有声书"}
		typeLabel := typeMap[itemType]
		if typeLabel != "" && displayName != "" {
			title = fmt.Sprintf("%s%s %s", actionText, typeLabel, displayName)
		} else {
			title = actionText
		}
	}

	texts := make([]string, 0, 6)
	if userName := strings.TrimSpace(asString(event.JSONObject["UserName"])); userName != "" {
		texts = append(texts, "用户："+userName)
	}
	if device := strings.TrimSpace(asString(event.JSONObject["DeviceName"])); device != "" {
		texts = append(texts, "设备："+device)
	} else if client := strings.TrimSpace(asString(event.JSONObject["Client"])); client != "" {
		texts = append(texts, "设备："+client)
	}
	if ip := strings.TrimSpace(asString(event.JSONObject["Ip"])); ip != "" {
		texts = append(texts, "IP地址："+ip)
	}
	if percentage := strings.TrimSpace(asString(event.JSONObject["Percentage"])); percentage != "" {
		texts = append(texts, "进度："+formatProgress(percentage)+"%")
	}
	if ratingLine := resolveTMDBRatingLine(tmdbInfo); ratingLine != "" {
		texts = append(texts, ratingLine)
	}
	if itemType == "TV" && event.SeasonID != nil && event.EpisodeID != nil {
		texts = append(texts, resolveEpisodeLine([]Event{event}))
	}
	if eventAction == "library.new" {
		texts = append(texts, resolveOverviewLine(strings.TrimSpace(asString(tmdbInfo["overview"]))))
	} else if ov := strings.TrimSpace(asString(tmdbInfo["overview"])); ov != "" {
		texts = append(texts, "📝 剧情: "+truncateText(ov, 240))
	}
	if eventAction == "library.new" || len(texts) > 0 {
		texts = appendTimeIfNeeded(texts)
	}
	imageURL := event.ImageURL
	if imageURL == "" {
		imageURL = resolveTMDBImage(tmdbInfo, true)
	}
	return title, strings.Join(texts, "\n"), imageURL
}

func (s *Service) HandleWebhook(ctx context.Context, data map[string]any, cfg config.Config) string {
	_ = ctx
	if !cfg.NotificationEnabled {
		return "notifier disabled"
	}
	event := parseEmbyWebhook(data)
	if event == nil {
		return "unparseable webhook"
	}
	if _, ok := webhookActions[event.Event]; !ok {
		return "unsupported event: " + event.Event
	}
	if len(cfg.NotifyTypes) > 0 {
		allowed := map[string]struct{}{}
		for _, t := range cfg.NotifyTypes {
			for _, part := range strings.Split(t, "|") {
				part = strings.TrimSpace(part)
				if part != "" {
					allowed[part] = struct{}{}
				}
			}
		}
		if _, ok := allowed[event.Event]; !ok {
			return "event type not allowed: " + event.Event
		}
	}

	dedupeKey := fmt.Sprintf("%s-%s-%s", event.ServerName, event.Event, event.ItemID)
	now := time.Now()

	s.mu.Lock()
	if len(s.dedupeCache) > 512 {
		for k, exp := range s.dedupeCache {
			if !exp.After(now) {
				delete(s.dedupeCache, k)
			}
		}
	}
	if exp, ok := s.dedupeCache[dedupeKey]; ok && exp.After(now) {
		s.mu.Unlock()
		return "duplicate"
	}
	s.dedupeCache[dedupeKey] = now.Add(aggregateDedupExpiration * time.Second)

	if cfg.AggregateEnabled && event.Event == "library.new" && event.ItemType == "TV" {
		if event.SeriesID != "" {
			seriesID := event.SeriesID
			s.pendingMessages[seriesID] = append(s.pendingMessages[seriesID], *event)
			if timer, ok := s.aggregateTimers[seriesID]; ok {
				timer.Stop()
			}
			timer := time.AfterFunc(time.Duration(cfg.AggregateTime)*time.Second, func() {
				s.sendAggregatedMessage(seriesID, cfg)
			})
			s.aggregateTimers[seriesID] = timer
			queueSize := len(s.pendingMessages[seriesID])
			s.mu.Unlock()
			return fmt.Sprintf("aggregated, queue size: %d", queueSize)
		}
	}
	s.mu.Unlock()

	title, text, imageURL := buildGenericMessage(*event, cfg.TmdbAPIKey)
	var imageBytes []byte
	imageName := "poster.jpg"
	if imageURL == "" {
		if downloaded, name, err := downloadEmbyImage(*event, cfg.EmbyServerURL, cfg.EmbyAPIKey); err == nil {
			imageBytes = downloaded
			if name != "" {
				imageName = name
			}
		}
	}
	_ = sendTelegram(cfg.TgToken, cfg.TgChatID, title, text, imageURL, imageBytes, imageName)
	return "ok"
}

func (s *Service) sendAggregatedMessage(seriesID string, cfg config.Config) {
	s.mu.Lock()
	events := append([]Event(nil), s.pendingMessages[seriesID]...)
	delete(s.pendingMessages, seriesID)
	delete(s.aggregateTimers, seriesID)
	s.mu.Unlock()
	if len(events) == 0 {
		return
	}
	title, text, imageURL := buildTVMessage(events, cfg.TmdbAPIKey)
	var imageBytes []byte
	imageName := "poster.jpg"
	if imageURL == "" {
		if downloaded, name, err := downloadEmbyImage(events[0], cfg.EmbyServerURL, cfg.EmbyAPIKey); err == nil {
			imageBytes = downloaded
			if name != "" {
				imageName = name
			}
		}
	}
	_ = sendTelegram(cfg.TgToken, cfg.TgChatID, title, text, imageURL, imageBytes, imageName)
}

func sendTelegram(token, chatID, title, text, imageURL string, imageBytes []byte, imageName string) bool {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(chatID) == "" {
		return false
	}
	client := &http.Client{Timeout: 30 * time.Second}
	caption := formatTelegramPayload(title, text, intPtr(1024))

	if len(imageBytes) > 0 {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		_ = writer.WriteField("chat_id", chatID)
		_ = writer.WriteField("caption", caption)
		_ = writer.WriteField("parse_mode", "HTML")
		part, err := writer.CreateFormFile("photo", imageName)
		if err == nil {
			_, _ = part.Write(imageBytes)
		}
		_ = writer.Close()

		req, err := http.NewRequest(http.MethodPost, "https://api.telegram.org/bot"+token+"/sendPhoto", &body)
		if err == nil {
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if resp, err := client.Do(req); err == nil {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return true
				}
			}
		}
	}

	if imageURL != "" {
		data := url.Values{}
		data.Set("chat_id", chatID)
		data.Set("photo", imageURL)
		data.Set("caption", caption)
		data.Set("parse_mode", "HTML")
		if resp, err := client.PostForm("https://api.telegram.org/bot"+token+"/sendPhoto", data); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
	}

	payload := formatTelegramPayload(title, text, nil)
	resp, err := client.PostForm("https://api.telegram.org/bot"+token+"/sendMessage", url.Values{
		"chat_id":    []string{chatID},
		"text":       []string{payload},
		"parse_mode": []string{"HTML"},
	})
	if err != nil {
		return false
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func downloadEmbyImage(event Event, serverURL, apiKey string) ([]byte, string, error) {
	if strings.TrimSpace(serverURL) == "" || strings.TrimSpace(apiKey) == "" {
		return nil, "", fmt.Errorf("emby not configured")
	}
	item := eventItem(event)
	if len(item) == 0 {
		return nil, "", fmt.Errorf("missing item")
	}
	client := emby.New(serverURL, apiKey)
	preferSeriesPrimary := event.ItemType == "TV"
	itemForImage := item
	if preferSeriesPrimary && strings.TrimSpace(asString(item["SeriesPrimaryImageTag"])) == "" && strings.TrimSpace(event.SeriesID) != "" {
		if seriesItem, err := client.GetItem(context.Background(), event.SeriesID); err == nil && len(seriesItem) > 0 {
			itemForImage = seriesItem
		}
	}
	apiPath := client.GetImageURL(itemForImage, preferSeriesPrimary)
	if apiPath == "" {
		if freshItem, err := client.GetItem(context.Background(), event.ItemID); err == nil && len(freshItem) > 0 {
			apiPath = client.GetImageURL(freshItem, preferSeriesPrimary)
		}
	}
	if apiPath == "" {
		return nil, "", fmt.Errorf("no image available")
	}

	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(serverURL, "/")+apiPath, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("X-Emby-Token", apiKey)
	clientHTTP := &http.Client{Timeout: 30 * time.Second}
	resp, err := clientHTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("emby image download failed: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	imageName := "poster.jpg"
	if strings.Contains(apiPath, "/Backdrop/") {
		imageName = "backdrop.jpg"
	}
	return body, imageName, nil
}

func intPtr(v int) *int { return &v }
