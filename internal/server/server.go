package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"embytool/internal/config"
	"embytool/internal/cover"
	"embytool/internal/emby"
	"embytool/internal/fonts"
	"embytool/internal/notifier"
	"embytool/internal/scheduler"
	"embytool/internal/version"
)

const (
	defaultPort = "8055"
	defaultAddr = ":" + defaultPort
)

type Server struct {
	mu          sync.RWMutex
	cfg         config.Config
	fontCache   *fonts.Cache
	coverSvc    *cover.Service
	notifierSvc *notifier.Service
	scheduler   *scheduler.Manager
	httpServer  *http.Server
	addr        string
	staticDir   string
	imagesDir   string
}

type libraryGenerateItem struct {
	Library string `json:"library"`
	cover.Result
}

func New(addr string) *Server {
	cfg := config.Load()
	fontCache := fonts.NewCache()
	return &Server{
		cfg:         cfg,
		fontCache:   fontCache,
		coverSvc:    cover.NewService(fontCache),
		notifierSvc: notifier.NewService(),
		scheduler:   scheduler.New(),
		addr:        normalizeAddr(addr),
		staticDir:   resolveAssetDir(filepath.Join("app", "static")),
		imagesDir:   resolveAssetDir("images"),
	}
}

func normalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	switch {
	case addr == "":
		return defaultAddr
	case strings.HasPrefix(addr, ":"):
		return addr
	case strings.Count(addr, ":") == 0:
		return ":" + addr
	default:
		return addr
	}
}

func (s *Server) Run(ctx context.Context) error {
	cfg := s.currentConfig()
	s.scheduler.Start(cfg.SchedulerEnabled, cfg.SchedulerCron, s.scheduledGenerate)

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/favicon.ico", s.handleFavicon)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(s.staticDir))))
	mux.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir(s.imagesDir))))
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/libraries", s.handleLibraries)
	mux.HandleFunc("/api/libraries/generate", s.handleLibrariesGenerate)
	mux.HandleFunc("/api/libraries/generate_all", s.handleLibrariesGenerateAll)
	mux.HandleFunc("/api/generate", s.handleGenerate)
	mux.HandleFunc("/api/scheduler/status", s.handleSchedulerStatus)
	mux.HandleFunc("/api/scheduler/restart", s.handleSchedulerRestart)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/mem", s.handleMem)
	mux.HandleFunc("/webhook/emby", s.handleWebhook)

	srv := &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}
	s.httpServer = srv

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.scheduler.Stop()
		s.notifierSvc.Stop()
		shutdownErr := srv.Shutdown(shutdownCtx)
		listenErr := <-errCh
		if shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed) {
			return shutdownErr
		}
		if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			return listenErr
		}
		return nil
	case err := <-errCh:
		s.scheduler.Stop()
		s.notifierSvc.Stop()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func (s *Server) currentConfig() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Server) updateConfig(cfg config.Config) {
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
}

func (s *Server) newEmbyClient() *emby.Client {
	cfg := s.currentConfig()
	return emby.New(cfg.EmbyServerURL, cfg.EmbyAPIKey)
}

func (s *Server) scheduledGenerate() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("定时任务发生 panic: %v", r)
		}
	}()

	cfg := config.Load()
	client := emby.New(cfg.EmbyServerURL, cfg.EmbyAPIKey)
	libraries, err := client.GetLibraries(context.Background())
	if err != nil {
		log.Printf("定时任务：获取媒体库失败: %v", err)
		return
	}

	selected := cfg.SelectedLibraries
	if len(selected) == 0 {
		selected = cfg.ScheduledLibraries
	}
	if len(selected) > 0 {
		libraries = filterLibrariesByIDs(libraries, selected)
	}
	if len(libraries) == 0 {
		log.Printf("定时任务：没有可用的媒体库")
		return
	}

	for _, library := range libraries {
		result := s.generateLibraryCover(context.Background(), client, library, cfg)
		log.Printf("定时任务 [%s] %s", asString(library["Name"]), result.Message)
		runtime.GC()
	}
	runtime.GC()
}

func filterLibrariesByIDs(libraries []map[string]any, ids []string) []map[string]any {
	if len(ids) == 0 {
		return libraries
	}
	selected := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			selected[id] = struct{}{}
		}
	}
	if len(selected) == 0 {
		return libraries
	}
	out := make([]map[string]any, 0, len(libraries))
	for _, library := range libraries {
		if _, ok := selected[itemID(library)]; ok {
			out = append(out, library)
		}
	}
	return out
}

func itemID(item map[string]any) string {
	if id := strings.TrimSpace(asString(item["Id"])); id != "" {
		return id
	}
	if id := strings.TrimSpace(asString(item["ItemId"])); id != "" {
		return id
	}
	return ""
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

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.staticDir, "index.html"))
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.staticDir, "favicon.ico"))
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": version.Get()})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.currentConfig().Map())
		return
	case http.MethodPost:
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
		return
	}

	var raw map[string]any
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "请求体不是合法 JSON"})
		return
	}

	cfg := config.Merge(s.currentConfig(), raw)
	if err := config.Save(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": fmt.Sprintf("保存配置失败: %v", err)})
		return
	}

	s.updateConfig(cfg)
	s.scheduler.Start(cfg.SchedulerEnabled, cfg.SchedulerCron, s.scheduledGenerate)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "配置已保存",
		"config":  cfg.Map(),
	})
}

func (s *Server) handleLibraries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	cfg := s.currentConfig()
	client := emby.New(cfg.EmbyServerURL, cfg.EmbyAPIKey)
	libraries, err := client.GetLibraries(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"message": fmt.Sprintf("连接 Emby 失败: %v", err),
		})
		return
	}

	items := make([]map[string]any, 0, len(libraries))
	for _, library := range libraries {
		items = append(items, map[string]any{
			"id":   itemID(library),
			"name": asString(library["Name"]),
			"type": firstNonEmpty(asString(library["CollectionType"]), asString(library["Type"])),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "libraries": items})
}

func (s *Server) handleLibrariesGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	var payload struct {
		LibraryIDs []string `json:"library_ids"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "请求体不是合法 JSON"})
		return
	}
	if len(payload.LibraryIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "请选择媒体库"})
		return
	}

	cfg := s.currentConfig()
	client := emby.New(cfg.EmbyServerURL, cfg.EmbyAPIKey)
	libraries, err := client.GetLibraries(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"message": fmt.Sprintf("连接 Emby 失败: %v", err),
		})
		return
	}

	selected := filterLibrariesByIDs(libraries, payload.LibraryIDs)
	if len(selected) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "message": "未找到指定媒体库"})
		return
	}

	results := make([]libraryGenerateItem, 0, len(selected))
	for _, library := range selected {
		result := s.generateLibraryCover(r.Context(), client, library, cfg)
		results = append(results, libraryGenerateItem{
			Library: asString(library["Name"]),
			Result:  result,
		})
		log.Printf("[%s] %s", asString(library["Name"]), result.Message)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"results": results,
	})
	runtime.GC()
}

func (s *Server) handleLibrariesGenerateAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	cfg := s.currentConfig()
	client := emby.New(cfg.EmbyServerURL, cfg.EmbyAPIKey)
	libraries, err := client.GetLibraries(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":      false,
			"message": fmt.Sprintf("连接 Emby 失败: %v", err),
		})
		return
	}

	selected := cfg.SelectedLibraries
	if len(selected) > 0 {
		libraries = filterLibrariesByIDs(libraries, selected)
	}
	if len(libraries) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "没有可用的媒体库"})
		return
	}

	results := make([]libraryGenerateItem, 0, len(libraries))
	for _, library := range libraries {
		result := s.generateLibraryCover(r.Context(), client, library, cfg)
		results = append(results, libraryGenerateItem{
			Library: asString(library["Name"]),
			Result:  result,
		})
		log.Printf("[%s] %s", asString(library["Name"]), result.Message)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"results": results,
	})
	runtime.GC()
}

func (s *Server) generateLibraryCover(ctx context.Context, client *emby.Client, library map[string]any, cfg config.Config) (result cover.Result) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("生成封面时发生 panic: %v", r)
			result = cover.Result{Ok: false, Message: fmt.Sprintf("内部异常: %v", r)}
		}
	}()
	result = s.coverSvc.GenerateForLibrary(ctx, client, library, cfg)
	return
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "请求体不是合法表单"})
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "未找到上传图片"})
		return
	}
	defer file.Close()

	cfg := s.currentConfig()
	outputDir := cfg.CoversOutput
	if strings.TrimSpace(outputDir) == "" {
		outputDir = "/data/covers_output"
	}
	tempDir := filepath.Join(outputDir, "tmp")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		ext = ".jpg"
	}
	inputFile, err := os.CreateTemp(tempDir, "input-*"+ext)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	inputPath := inputFile.Name()
	if _, err := io.Copy(inputFile, file); err != nil {
		inputFile.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if err := inputFile.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}

	params := cover.ManualParams{
		TitleZh:        formString(r, "title_zh", ""),
		TitleEn:        formString(r, "title_en", ""),
		CoverStyle:     formString(r, "cover_style", cfg.CoverStyle),
		BlurSize:       formInt(r, "blur_size", 50),
		ColorRatio:     formFloat(r, "color_ratio", 0.8),
		ZhFontSize:     formFloat(r, "zh_font_size", 1.0),
		EnFontSize:     formFloat(r, "en_font_size", 1.0),
		ShowItemCount:  formBool(r, "show_item_count", false),
		BadgeStyle:     formString(r, "badge_style", "badge"),
		BadgeSizeRatio: formFloat(r, "badge_size_ratio", 0.12),
		ItemCount:      formInt(r, "item_count", 0),
	}

	imageBytes, err := s.coverSvc.GenerateManual(r.Context(), inputPath, cfg, params)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.HasPrefix(err.Error(), "未知风格:") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	if len(imageBytes) == 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": "封面生成失败"})
		return
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}
	outputPath := filepath.Join(outputDir, "cover.png")
	if err := os.WriteFile(outputPath, imageBytes, 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "message": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"image_base64": "data:image/png;base64," + encodeBase64(imageBytes),
	})
	runtime.GC()
}

func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	cfg := s.currentConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"running":  s.scheduler.Running(),
		"next_run": s.scheduler.NextRun(),
		"enabled":  cfg.SchedulerEnabled,
		"cron":     cfg.SchedulerCron,
	})
}

func (s *Server) handleSchedulerRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	cfg := s.currentConfig()
	s.scheduler.Start(cfg.SchedulerEnabled, cfg.SchedulerCron, s.scheduledGenerate)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"running":  s.scheduler.Running(),
		"next_run": s.scheduler.NextRun(),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleMem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	rss, ok := memoryRSSMB()
	var rssValue any
	if ok {
		rssValue = rss
	}
	dedupeSize, pendingKeys := s.notifierSvc.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"rss_mb":              rssValue,
		"font_cache_size":     s.fontCache.Size(),
		"dedupe_cache_size":   dedupeSize,
		"pending_messages_keys": pendingKeys,
	})
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var data map[string]any
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid JSON"})
		return
	}
	result := s.notifierSvc.HandleWebhook(r.Context(), data, s.currentConfig())
	log.Printf("Webhook 处理结果: %s", result)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": result})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveAssetDir(rel string) string {
	candidates := []string{
		rel,
		filepath.Clean(rel),
	}
	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for i := 0; i < 4; i++ {
			candidates = append(candidates, filepath.Join(dir, rel))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 4; i++ {
			candidates = append(candidates, filepath.Join(dir, rel))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return rel
}

func formString(r *http.Request, key, fallback string) string {
	value := strings.TrimSpace(r.FormValue(key))
	if value == "" {
		return fallback
	}
	return value
}

func formBool(r *http.Request, key string, fallback bool) bool {
	value := strings.TrimSpace(r.FormValue(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func formInt(r *http.Request, key string, fallback int) int {
	value := strings.TrimSpace(r.FormValue(key))
	if value == "" {
		return fallback
	}
	if n, err := strconv.Atoi(value); err == nil {
		return n
	}
	return fallback
}

func formFloat(r *http.Request, key string, fallback float64) float64 {
	value := strings.TrimSpace(r.FormValue(key))
	if value == "" {
		return fallback
	}
	if n, err := strconv.ParseFloat(value, 64); err == nil {
		return n
	}
	return fallback
}

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func memoryRSSMB() (float64, bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, false
	}
	rss := float64(usage.Maxrss)
	switch runtime.GOOS {
	case "darwin", "ios":
		rss = rss / 1024.0 / 1024.0
	default:
		rss = rss / 1024.0
	}
	rss = float64(int(rss*10+0.5)) / 10
	return rss, true
}
