package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const ConfigFile = "/data/config.json"

type TitleOverride struct {
	Zh string `json:"zh"`
	En string `json:"en"`
}

type Config struct {
	EmbyServerURL string `json:"emby_server_url"`
	EmbyAPIKey    string `json:"emby_api_key"`
	EmbyUserID    string `json:"emby_user_id"`

	SelectedLibraries []string `json:"selected_libraries"`
	CoverStyle        string   `json:"cover_style"`
	SortBy            string   `json:"sort_by"`
	CoversInput       string   `json:"covers_input"`
	CoversOutput      string   `json:"covers_output"`
	ZhFontPath        string   `json:"zh_font_path"`
	EnFontPath        string   `json:"en_font_path"`

	CustomLibraryTitlesEnabled bool                     `json:"custom_library_titles_enabled"`
	LibraryTitleOverrides      map[string]TitleOverride `json:"library_title_overrides"`

	SingleUsePrimary     bool    `json:"single_use_primary"`
	SingleBlurSize       int     `json:"single_blur_size"`
	SingleColorRatio     float64 `json:"single_color_ratio"`
	SingleZhFontSize     float64 `json:"single_zh_font_size"`
	SingleEnFontSize     float64 `json:"single_en_font_size"`
	SingleShowItemCount  bool    `json:"single_show_item_count"`
	SingleBadgeStyle     string  `json:"single_badge_style"`
	SingleBadgeSizeRatio float64 `json:"single_badge_size_ratio"`

	Multi1Blur          bool    `json:"multi_1_blur"`
	Multi1UsePrimary    bool    `json:"multi_1_use_primary"`
	MultiBlurSize       int     `json:"multi_blur_size"`
	MultiColorRatio     float64 `json:"multi_color_ratio"`
	MultiZhFontSize     float64 `json:"multi_zh_font_size"`
	MultiEnFontSize     float64 `json:"multi_en_font_size"`
	MultiShowItemCount  bool    `json:"multi_show_item_count"`
	MultiBadgeStyle     string  `json:"multi_badge_style"`
	MultiBadgeSizeRatio float64 `json:"multi_badge_size_ratio"`

	NotificationEnabled bool     `json:"notification_enabled"`
	TgToken             string   `json:"tg_token"`
	TgChatID            string   `json:"tg_chat_id"`
	TmdbAPIKey          string   `json:"tmdb_api_key"`
	NotifyTypes         []string `json:"notify_types"`

	AggregateEnabled      bool     `json:"aggregate_enabled"`
	AggregateTime         int      `json:"aggregate_time"`
	NewImportCoverWindow  int      `json:"new_import_cover_window"`
	NewImportCoverEnabled bool     `json:"new_import_cover_enabled"`
	SchedulerEnabled      bool     `json:"scheduler_enabled"`
	SchedulerCron         string   `json:"scheduler_cron"`
	ScheduledLibraries    []string `json:"scheduled_libraries"`
}

func Default() Config {
	return Config{
		EmbyServerURL: "",
		EmbyAPIKey:    "",
		EmbyUserID:    "",

		SelectedLibraries: []string{},
		CoverStyle:        "single_1",
		SortBy:            "Random",
		CoversInput:       "/data/input",
		CoversOutput:      "/data/covers_output",
		ZhFontPath:        "",
		EnFontPath:        "",

		CustomLibraryTitlesEnabled: false,
		LibraryTitleOverrides:      map[string]TitleOverride{},

		SingleUsePrimary:     true,
		SingleBlurSize:       50,
		SingleColorRatio:     0.8,
		SingleZhFontSize:     1.0,
		SingleEnFontSize:     1.0,
		SingleShowItemCount:  false,
		SingleBadgeStyle:     "badge",
		SingleBadgeSizeRatio: 0.12,

		Multi1Blur:          false,
		Multi1UsePrimary:    true,
		MultiBlurSize:       50,
		MultiColorRatio:     0.8,
		MultiZhFontSize:     1.0,
		MultiEnFontSize:     1.0,
		MultiShowItemCount:  false,
		MultiBadgeStyle:     "badge",
		MultiBadgeSizeRatio: 0.12,

		NotificationEnabled: false,
		TgToken:             "",
		TgChatID:            "",
		TmdbAPIKey:          "",
		NotifyTypes:         []string{},

		AggregateEnabled:      true,
		AggregateTime:         15,
		NewImportCoverWindow:  300,
		NewImportCoverEnabled: false,
		SchedulerEnabled:      false,
		SchedulerCron:         "0 4 * * *",
		ScheduledLibraries:    []string{},
	}
}

func (c Config) Map() map[string]any {
	data, _ := json.Marshal(c)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func Load() Config {
	raw, err := readRaw(ConfigFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("加载配置失败: %v", err)
		}
		return Default()
	}
	cfg := Normalize(raw)
	log.Printf("配置已从 %s 加载", ConfigFile)
	return cfg
}

func Save(cfg Config) error {
	normalized := Normalize(cfg.Map())
	if err := os.MkdirAll(filepath.Dir(ConfigFile), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(ConfigFile, data, 0o644); err != nil {
		return err
	}
	log.Printf("配置已保存到 %s", ConfigFile)
	return nil
}

func Merge(base Config, raw map[string]any) Config {
	merged := base.Map()
	for k, v := range raw {
		merged[k] = v
	}
	return Normalize(merged)
}

func Normalize(raw map[string]any) Config {
	defaults := Default()
	cfg := Default()
	if raw == nil {
		return cfg
	}

	if v, ok := raw["emby_server_url"]; ok {
		cfg.EmbyServerURL = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["emby_api_key"]; ok {
		cfg.EmbyAPIKey = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["emby_user_id"]; ok {
		cfg.EmbyUserID = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["selected_libraries"]; ok {
		cfg.SelectedLibraries = cleanList(v)
	}
	if v, ok := raw["cover_style"]; ok {
		cfg.CoverStyle = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["sort_by"]; ok {
		cfg.SortBy = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["covers_input"]; ok {
		cfg.CoversInput = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["covers_output"]; ok {
		cfg.CoversOutput = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["zh_font_path"]; ok {
		cfg.ZhFontPath = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["en_font_path"]; ok {
		cfg.EnFontPath = strings.TrimSpace(asString(v))
	}

	if v, ok := raw["custom_library_titles_enabled"]; ok {
		cfg.CustomLibraryTitlesEnabled = asBool(v, cfg.CustomLibraryTitlesEnabled)
	}
	if v, ok := raw["library_title_overrides"]; ok {
		cfg.LibraryTitleOverrides = cleanTitleOverrides(v)
	}

	if v, ok := raw["single_use_primary"]; ok {
		cfg.SingleUsePrimary = asBool(v, cfg.SingleUsePrimary)
	}
	if _, ok := raw["single_blur_size"]; ok {
		cfg.SingleBlurSize = asInt(raw["single_blur_size"], cfg.SingleBlurSize)
	} else if v, ok := raw["blur_size"]; ok {
		cfg.SingleBlurSize = asInt(v, cfg.SingleBlurSize)
	}
	if _, ok := raw["single_color_ratio"]; ok {
		cfg.SingleColorRatio = asFloat(raw["single_color_ratio"], cfg.SingleColorRatio)
	} else if v, ok := raw["color_ratio"]; ok {
		cfg.SingleColorRatio = asFloat(v, cfg.SingleColorRatio)
	}
	if _, ok := raw["single_zh_font_size"]; ok {
		cfg.SingleZhFontSize = asFloat(raw["single_zh_font_size"], cfg.SingleZhFontSize)
	} else if v, ok := raw["zh_font_size"]; ok {
		cfg.SingleZhFontSize = asFloat(v, cfg.SingleZhFontSize)
	}
	if _, ok := raw["single_en_font_size"]; ok {
		cfg.SingleEnFontSize = asFloat(raw["single_en_font_size"], cfg.SingleEnFontSize)
	} else if v, ok := raw["en_font_size"]; ok {
		cfg.SingleEnFontSize = asFloat(v, cfg.SingleEnFontSize)
	}
	if _, ok := raw["single_show_item_count"]; ok {
		cfg.SingleShowItemCount = asBool(raw["single_show_item_count"], cfg.SingleShowItemCount)
	} else if v, ok := raw["show_item_count"]; ok {
		cfg.SingleShowItemCount = asBool(v, cfg.SingleShowItemCount)
	}
	if _, ok := raw["single_badge_style"]; ok {
		cfg.SingleBadgeStyle = strings.TrimSpace(asString(raw["single_badge_style"]))
	} else if v, ok := raw["badge_style"]; ok {
		cfg.SingleBadgeStyle = strings.TrimSpace(asString(v))
	}
	if _, ok := raw["single_badge_size_ratio"]; ok {
		cfg.SingleBadgeSizeRatio = asFloat(raw["single_badge_size_ratio"], cfg.SingleBadgeSizeRatio)
	} else if v, ok := raw["badge_size_ratio"]; ok {
		cfg.SingleBadgeSizeRatio = asFloat(v, cfg.SingleBadgeSizeRatio)
	}

	if v, ok := raw["multi_1_blur"]; ok {
		cfg.Multi1Blur = asBool(v, cfg.Multi1Blur)
	}
	if v, ok := raw["multi_1_use_primary"]; ok {
		cfg.Multi1UsePrimary = asBool(v, cfg.Multi1UsePrimary)
	} else if v, ok := raw["use_primary"]; ok {
		cfg.Multi1UsePrimary = asBool(v, cfg.Multi1UsePrimary)
	}
	if _, ok := raw["multi_blur_size"]; ok {
		cfg.MultiBlurSize = asInt(raw["multi_blur_size"], cfg.MultiBlurSize)
	} else if v, ok := raw["blur_size_multi_1"]; ok {
		cfg.MultiBlurSize = asInt(v, cfg.MultiBlurSize)
	}
	if _, ok := raw["multi_color_ratio"]; ok {
		cfg.MultiColorRatio = asFloat(raw["multi_color_ratio"], cfg.MultiColorRatio)
	} else if v, ok := raw["color_ratio_multi_1"]; ok {
		cfg.MultiColorRatio = asFloat(v, cfg.MultiColorRatio)
	}
	if _, ok := raw["multi_zh_font_size"]; ok {
		cfg.MultiZhFontSize = asFloat(raw["multi_zh_font_size"], cfg.MultiZhFontSize)
	} else if v, ok := raw["zh_font_size_multi_1"]; ok {
		cfg.MultiZhFontSize = asFloat(v, cfg.MultiZhFontSize)
	}
	if _, ok := raw["multi_en_font_size"]; ok {
		cfg.MultiEnFontSize = asFloat(raw["multi_en_font_size"], cfg.MultiEnFontSize)
	} else if v, ok := raw["en_font_size_multi_1"]; ok {
		cfg.MultiEnFontSize = asFloat(v, cfg.MultiEnFontSize)
	}
	if _, ok := raw["multi_show_item_count"]; ok {
		cfg.MultiShowItemCount = asBool(raw["multi_show_item_count"], cfg.MultiShowItemCount)
	} else if v, ok := raw["show_item_count"]; ok {
		cfg.MultiShowItemCount = asBool(v, cfg.MultiShowItemCount)
	}
	if _, ok := raw["multi_badge_style"]; ok {
		cfg.MultiBadgeStyle = strings.TrimSpace(asString(raw["multi_badge_style"]))
	} else if v, ok := raw["badge_style"]; ok {
		cfg.MultiBadgeStyle = strings.TrimSpace(asString(v))
	}
	if _, ok := raw["multi_badge_size_ratio"]; ok {
		cfg.MultiBadgeSizeRatio = asFloat(raw["multi_badge_size_ratio"], cfg.MultiBadgeSizeRatio)
	} else if v, ok := raw["badge_size_ratio"]; ok {
		cfg.MultiBadgeSizeRatio = asFloat(v, cfg.MultiBadgeSizeRatio)
	}

	if v, ok := raw["notification_enabled"]; ok {
		cfg.NotificationEnabled = asBool(v, cfg.NotificationEnabled)
	}
	if v, ok := raw["tg_token"]; ok {
		cfg.TgToken = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["tg_chat_id"]; ok {
		cfg.TgChatID = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["tmdb_api_key"]; ok {
		cfg.TmdbAPIKey = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["notify_types"]; ok {
		cfg.NotifyTypes = cleanList(v)
	}

	if v, ok := raw["aggregate_enabled"]; ok {
		cfg.AggregateEnabled = asBool(v, cfg.AggregateEnabled)
	}
	if v, ok := raw["aggregate_time"]; ok {
		cfg.AggregateTime = asInt(v, cfg.AggregateTime)
	}
	if v, ok := raw["new_import_cover_window"]; ok {
		cfg.NewImportCoverWindow = asInt(v, cfg.NewImportCoverWindow)
	}
	if v, ok := raw["new_import_cover_enabled"]; ok {
		cfg.NewImportCoverEnabled = asBool(v, cfg.NewImportCoverEnabled)
	}
	if v, ok := raw["scheduler_enabled"]; ok {
		cfg.SchedulerEnabled = asBool(v, cfg.SchedulerEnabled)
	}
	if v, ok := raw["scheduler_cron"]; ok {
		cfg.SchedulerCron = strings.TrimSpace(asString(v))
	}
	if v, ok := raw["scheduled_libraries"]; ok {
		cfg.ScheduledLibraries = cleanList(v)
	}

	cfg.SelectedLibraries = cleanList(cfg.SelectedLibraries)
	cfg.NotifyTypes = cleanList(cfg.NotifyTypes)
	cfg.ScheduledLibraries = cleanList(cfg.ScheduledLibraries)
	cfg.LibraryTitleOverrides = cleanTitleOverrides(cfg.LibraryTitleOverrides)

	cfg.EmbyServerURL = strings.TrimSpace(cfg.EmbyServerURL)
	cfg.EmbyAPIKey = strings.TrimSpace(cfg.EmbyAPIKey)
	cfg.EmbyUserID = strings.TrimSpace(cfg.EmbyUserID)
	cfg.CoverStyle = strings.TrimSpace(cfg.CoverStyle)
	cfg.SortBy = strings.TrimSpace(cfg.SortBy)
	cfg.CoversInput = strings.TrimSpace(cfg.CoversInput)
	cfg.CoversOutput = strings.TrimSpace(cfg.CoversOutput)
	cfg.ZhFontPath = strings.TrimSpace(cfg.ZhFontPath)
	cfg.EnFontPath = strings.TrimSpace(cfg.EnFontPath)
	cfg.SingleBadgeStyle = strings.TrimSpace(cfg.SingleBadgeStyle)
	cfg.MultiBadgeStyle = strings.TrimSpace(cfg.MultiBadgeStyle)
	cfg.TgToken = strings.TrimSpace(cfg.TgToken)
	cfg.TgChatID = strings.TrimSpace(cfg.TgChatID)
	cfg.TmdbAPIKey = strings.TrimSpace(cfg.TmdbAPIKey)
	cfg.SchedulerCron = strings.TrimSpace(cfg.SchedulerCron)

	if cfg.CoverStyle == "" {
		cfg.CoverStyle = defaults.CoverStyle
	}
	if cfg.SortBy == "" {
		cfg.SortBy = defaults.SortBy
	}
	if cfg.CoversInput == "" {
		cfg.CoversInput = defaults.CoversInput
	}
	if cfg.CoversOutput == "" {
		cfg.CoversOutput = defaults.CoversOutput
	}
	if cfg.SingleBadgeStyle == "" {
		cfg.SingleBadgeStyle = defaults.SingleBadgeStyle
	}
	if cfg.MultiBadgeStyle == "" {
		cfg.MultiBadgeStyle = defaults.MultiBadgeStyle
	}
	if cfg.SchedulerCron == "" {
		cfg.SchedulerCron = defaults.SchedulerCron
	}
	if cfg.NewImportCoverWindow <= 0 {
		cfg.NewImportCoverWindow = defaults.NewImportCoverWindow
	}

	if cfg.LibraryTitleOverrides == nil {
		cfg.LibraryTitleOverrides = map[string]TitleOverride{}
	}
	if cfg.SelectedLibraries == nil {
		cfg.SelectedLibraries = []string{}
	}
	if cfg.ScheduledLibraries == nil {
		cfg.ScheduledLibraries = []string{}
	}
	if cfg.NotifyTypes == nil {
		cfg.NotifyTypes = []string{}
	}

	return cfg
}

func readRaw(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func asBool(v any, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		default:
			return strings.TrimSpace(t) != ""
		}
	case float64:
		return t != 0
	case float32:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case json.Number:
		n, err := t.Int64()
		if err == nil {
			return n != 0
		}
		f, err := t.Float64()
		if err == nil {
			return f != 0
		}
		return def
	case nil:
		return def
	default:
		return def
	}
}

func asFloat(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int32:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, err := t.Float64()
		if err == nil {
			return f
		}
		return def
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return def
		}
		var out float64
		if _, err := fmt.Sscanf(s, "%f", &out); err == nil {
			return out
		}
		return def
	case nil:
		return def
	default:
		return def
	}
}

func asInt(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	case json.Number:
		n, err := t.Int64()
		if err == nil {
			return int(n)
		}
		f, err := t.Float64()
		if err == nil {
			return int(f)
		}
		return def
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return def
		}
		var out int
		if _, err := fmt.Sscanf(s, "%d", &out); err == nil {
			return out
		}
		var f float64
		if _, err := fmt.Sscanf(s, "%f", &f); err == nil {
			return int(f)
		}
		return def
	case nil:
		return def
	default:
		return def
	}
}

func cleanList(v any) []string {
	if v == nil {
		return []string{}
	}
	switch t := v.(type) {
	case []string:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(asString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if s := strings.TrimSpace(t); s != "" {
			return []string{s}
		}
		return []string{}
	default:
		if s := strings.TrimSpace(asString(v)); s != "" {
			return []string{s}
		}
		return []string{}
	}
}

func cleanTitleOverrides(v any) map[string]TitleOverride {
	if v == nil {
		return map[string]TitleOverride{}
	}
	out := map[string]TitleOverride{}
	switch t := v.(type) {
	case map[string]TitleOverride:
		for k, item := range t {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			out[key] = TitleOverride{Zh: strings.TrimSpace(item.Zh), En: strings.TrimSpace(item.En)}
		}
	case map[string]any:
		for k, item := range t {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			out[key] = TitleOverride{
				Zh: strings.TrimSpace(asString(m["zh"])),
				En: strings.TrimSpace(asString(m["en"])),
			}
		}
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return out
		}
		var tmp map[string]map[string]any
		if err := json.Unmarshal(data, &tmp); err != nil {
			return out
		}
		for k, item := range tmp {
			key := strings.TrimSpace(k)
			if key == "" {
				continue
			}
			out[key] = TitleOverride{
				Zh: strings.TrimSpace(asString(item["zh"])),
				En: strings.TrimSpace(asString(item["en"])),
			}
		}
	}
	return out
}
