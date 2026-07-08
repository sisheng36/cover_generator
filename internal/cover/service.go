package cover

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"embytool/internal/config"
	"embytool/internal/emby"
	"embytool/internal/fonts"
)

type Service struct {
	fonts *fonts.Cache
}

type FontPaths struct {
	Zh      string
	En      string
	ZhMulti string
	EnMulti string
}

type ManualParams struct {
	TitleZh        string
	TitleEn        string
	CoverStyle     string
	BlurSize       int
	ColorRatio     float64
	ZhFontSize     float64
	EnFontSize     float64
	ShowItemCount  bool
	BadgeStyle     string
	BadgeSizeRatio float64
	ItemCount      int
}

type Result struct {
	Ok        bool   `json:"ok"`
	Message   string `json:"message"`
	StyleName string `json:"style_name,omitempty"`
}

type styleSettings struct {
	UsePrimary     bool
	BlurSize       int
	ColorRatio     float64
	ZhFontSize     float64
	EnFontSize     float64
	ShowItemCount  bool
	BadgeStyle     string
	BadgeSizeRatio float64
	IsBlur         bool
}

type candidateItem struct {
	item   map[string]any
	imgURL string
}

var styleNames = map[string]string{
	"single_1": "单图风格 1",
	"single_2": "单图风格 2",
	"multi_1":  "多图风格 1",
}

var posterFilenames = []string{"poster.jpg", "poster.jpeg", "poster.png", "poster.webp"}

func NewService(fonts *fonts.Cache) *Service {
	return &Service{fonts: fonts}
}

func ResolveFontPaths(cfg config.Config) FontPaths {
	zhDefault := filepath.Join("fonts", "zh_font.ttf")
	enDefault := filepath.Join("fonts", "en_font.ttf")
	zhMultiDefault := filepath.Join("fonts", "zh_font_multi_1.ttf")
	enMultiDefault := filepath.Join("fonts", "en_font_multi_1.otf")

	zh := zhDefault
	if trimmed := strings.TrimSpace(cfg.ZhFontPath); trimmed != "" && fileExists(trimmed) {
		zh = trimmed
	}
	en := enDefault
	if trimmed := strings.TrimSpace(cfg.EnFontPath); trimmed != "" && fileExists(trimmed) {
		en = trimmed
	}

	zhMulti := zhMultiDefault
	if trimmed := strings.TrimSpace(cfg.ZhFontPath); trimmed != "" && fileExists(trimmed) {
		zhMulti = trimmed
	}
	enMulti := enMultiDefault
	if trimmed := strings.TrimSpace(cfg.EnFontPath); trimmed != "" && fileExists(trimmed) {
		enMulti = trimmed
	}

	return FontPaths{Zh: zh, En: en, ZhMulti: zhMulti, EnMulti: enMulti}
}

func (s *Service) GenerateManual(ctx context.Context, imagePath string, cfg config.Config, params ManualParams) ([]byte, error) {
	_ = ctx
	fonts := ResolveFontPaths(cfg)
	badge := badgeConfig{
		Show:      params.ShowItemCount,
		Style:     params.BadgeStyle,
		SizeRatio: params.BadgeSizeRatio,
	}
	switch params.CoverStyle {
	case "single_1":
		return createStyleSingle1(
			imagePath,
			[2]string{params.TitleZh, params.TitleEn},
			[2]string{fonts.Zh, fonts.En},
			s.fonts,
			[2]float64{params.ZhFontSize, params.EnFontSize},
			params.BlurSize,
			params.ColorRatio,
			params.ItemCount,
			badge,
		)
	case "single_2":
		return createStyleSingle2(
			imagePath,
			[2]string{params.TitleZh, params.TitleEn},
			[2]string{fonts.Zh, fonts.En},
			s.fonts,
			[2]float64{params.ZhFontSize, params.EnFontSize},
			params.BlurSize,
			params.ColorRatio,
			params.ItemCount,
			badge,
		)
	case "multi_1":
		outputDir := cfg.CoversOutput
		if strings.TrimSpace(outputDir) == "" {
			outputDir = "/data/covers_output"
		}
		tempDir := filepath.Join(outputDir, "tmp", "multi_temp")
		if err := os.MkdirAll(tempDir, 0o755); err != nil {
			return nil, err
		}
		inputData, err := os.ReadFile(imagePath)
		if err != nil {
			return nil, err
		}
		for i := 1; i <= 9; i++ {
			target := filepath.Join(tempDir, fmt.Sprintf("%d.jpg", i))
			if err := os.WriteFile(target, inputData, 0o644); err != nil {
				return nil, err
			}
		}
		return createStyleMulti1(
			tempDir,
			[2]string{params.TitleZh, params.TitleEn},
			[2]string{fonts.ZhMulti, fonts.EnMulti},
			s.fonts,
			[2]float64{params.ZhFontSize, params.EnFontSize},
			false,
			params.BlurSize,
			params.ColorRatio,
			params.ItemCount,
			badge,
		)
	default:
		return nil, fmt.Errorf("未知风格: %s", params.CoverStyle)
	}
}

func (s *Service) GenerateForLibrary(ctx context.Context, client *emby.Client, library map[string]any, cfg config.Config) Result {
	libraryName, _ := library["Name"].(string)
	libraryID := itemID(library)
	if libraryName == "" {
		libraryName = "Unknown"
	}
	settings := getStyleSettings(cfg)
	fonts := ResolveFontPaths(cfg)
	badge := badgeConfig{
		Show:      settings.ShowItemCount,
		Style:     settings.BadgeStyle,
		SizeRatio: settings.BadgeSizeRatio,
	}
	itemCount := countValue(library["ChildCount"])
	if itemCount == 0 {
		itemCount = countValue(library["RecursiveItemCount"])
	}

	required := 1
	if strings.HasPrefix(cfg.CoverStyle, "multi") {
		required = 9
	}
	valid, err := s.collectCandidateItems(ctx, client, library, settings, required, cfg.SortBy)
	if err != nil {
		return Result{Ok: false, Message: err.Error()}
	}
	if len(valid) == 0 {
		return Result{Ok: false, Message: "媒体库没有可用的项目"}
	}
	if strings.EqualFold(cfg.SortBy, "Random") {
		rand.Shuffle(len(valid), func(i, j int) { valid[i], valid[j] = valid[j], valid[i] })
	}
	if len(valid) > required {
		valid = valid[:required]
	}

	sourceDir, err := s.prepareSourceDir(cfg, libraryName)
	if err != nil {
		return Result{Ok: false, Message: err.Error()}
	}

	imagePaths := make([]string, 0, len(valid))
	for idx, candidate := range valid {
		savePath := filepath.Join(sourceDir, fmt.Sprintf("%d.jpg", idx+1))
		resultPath, err := s.copyLocalPoster(candidate.item, savePath)
		if err != nil {
			return Result{Ok: false, Message: err.Error()}
		}
		if resultPath == "" && candidate.imgURL != "" {
			resultPath, err = client.DownloadImage(ctx, candidate.imgURL, savePath)
			if err != nil {
				log.Printf("下载媒体封面失败 %s: %v", candidate.imgURL, err)
				continue
			}
		}
		if resultPath != "" {
			imagePaths = append(imagePaths, resultPath)
		}
	}
	if len(imagePaths) == 0 {
		return Result{Ok: false, Message: "图片下载全部失败"}
	}

	if cfg.CoverStyle == "multi_1" && len(imagePaths) < 9 {
		for i := len(imagePaths); i < 9; i++ {
			target := filepath.Join(sourceDir, fmt.Sprintf("%d.jpg", i+1))
			if err := copyFile(imagePaths[0], target); err != nil {
				return Result{Ok: false, Message: err.Error()}
			}
		}
	}

	titleZh, titleEn := resolveLibraryTitles(library, cfg)

	var imageBytes []byte
	switch cfg.CoverStyle {
	case "single_1":
		imageBytes, err = createStyleSingle1(
			imagePaths[0],
			[2]string{titleZh, titleEn},
			[2]string{fonts.Zh, fonts.En},
			s.fonts,
			[2]float64{settings.ZhFontSize, settings.EnFontSize},
			settings.BlurSize,
			settings.ColorRatio,
			itemCount,
			badge,
		)
	case "single_2":
		imageBytes, err = createStyleSingle2(
			imagePaths[0],
			[2]string{titleZh, titleEn},
			[2]string{fonts.Zh, fonts.En},
			s.fonts,
			[2]float64{settings.ZhFontSize, settings.EnFontSize},
			settings.BlurSize,
			settings.ColorRatio,
			itemCount,
			badge,
		)
	case "multi_1":
		imageBytes, err = createStyleMulti1(
			sourceDir,
			[2]string{titleZh, titleEn},
			[2]string{fonts.ZhMulti, fonts.EnMulti},
			s.fonts,
			[2]float64{settings.ZhFontSize, settings.EnFontSize},
			settings.IsBlur,
			settings.BlurSize,
			settings.ColorRatio,
			itemCount,
			badge,
		)
	default:
		return Result{Ok: false, Message: fmt.Sprintf("未知风格: %s", cfg.CoverStyle)}
	}
	if err != nil {
		return Result{Ok: false, Message: err.Error()}
	}
	if len(imageBytes) == 0 {
		return Result{Ok: false, Message: "封面生成失败"}
	}

	uploadOK, uploadErr := client.UploadLibraryImage(ctx, libraryID, imageBytes)
	if uploadErr != nil {
		uploadOK = false
	}

	if cfg.CoversOutput != "" {
		outDir := cfg.CoversOutput
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return Result{Ok: false, Message: err.Error()}
		}
		outPath := filepath.Join(outDir, sanitizeName(libraryName)+".jpg")
		if err := os.WriteFile(outPath, imageBytes, 0o644); err != nil {
			return Result{Ok: false, Message: err.Error()}
		}
	}

	styleName := styleNames[cfg.CoverStyle]
	if styleName == "" {
		styleName = cfg.CoverStyle
	}
	if uploadOK {
		return Result{Ok: true, Message: fmt.Sprintf("'%s' 封面已更新", libraryName), StyleName: styleName}
	}
	if uploadErr != nil {
		return Result{Ok: true, Message: "封面已生成但上传失败", StyleName: styleName}
	}
	return Result{Ok: true, Message: "封面已生成但上传失败", StyleName: styleName}
}

func getStyleSettings(cfg config.Config) styleSettings {
	switch cfg.CoverStyle {
	case "multi_1":
		return styleSettings{
			UsePrimary:     cfg.Multi1UsePrimary,
			BlurSize:       cfg.MultiBlurSize,
			ColorRatio:     cfg.MultiColorRatio,
			ZhFontSize:     cfg.MultiZhFontSize,
			EnFontSize:     cfg.MultiEnFontSize,
			ShowItemCount:  cfg.MultiShowItemCount,
			BadgeStyle:     cfg.MultiBadgeStyle,
			BadgeSizeRatio: cfg.MultiBadgeSizeRatio,
			IsBlur:         cfg.Multi1Blur,
		}
	default:
		return styleSettings{
			UsePrimary:     cfg.SingleUsePrimary,
			BlurSize:       cfg.SingleBlurSize,
			ColorRatio:     cfg.SingleColorRatio,
			ZhFontSize:     cfg.SingleZhFontSize,
			EnFontSize:     cfg.SingleEnFontSize,
			ShowItemCount:  cfg.SingleShowItemCount,
			BadgeStyle:     cfg.SingleBadgeStyle,
			BadgeSizeRatio: cfg.SingleBadgeSizeRatio,
			IsBlur:         false,
		}
	}
}

func resolveLibraryTitles(library map[string]any, cfg config.Config) (string, string) {
	libraryID := itemID(library)
	libraryName, _ := library["Name"].(string)
	if libraryName == "" {
		libraryName = "Unknown"
	}
	if !cfg.CustomLibraryTitlesEnabled {
		return libraryName, ""
	}
	if override, ok := cfg.LibraryTitleOverrides[libraryID]; ok {
		titleZh := strings.TrimSpace(override.Zh)
		if titleZh == "" {
			titleZh = libraryName
		}
		return titleZh, strings.TrimSpace(override.En)
	}
	for key, override := range cfg.LibraryTitleOverrides {
		if key == libraryName {
			titleZh := strings.TrimSpace(override.Zh)
			if titleZh == "" {
				titleZh = libraryName
			}
			return titleZh, strings.TrimSpace(override.En)
		}
	}
	return libraryName, ""
}

func (s *Service) prepareSourceDir(cfg config.Config, libraryName string) (string, error) {
	root := cfg.CoversInput
	if strings.TrimSpace(root) == "" {
		root = "/data/input"
	}
	sourceDir := filepath.Join(root, sanitizeName(libraryName))
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		return "", err
	}
	return sourceDir, nil
}

func (s *Service) copyLocalPoster(item map[string]any, savePath string) (string, error) {
	for _, candidate := range localPosterCandidates(item) {
		if err := copyFile(candidate, savePath); err == nil {
			log.Printf("命中本地海报: %s", candidate)
			return savePath, nil
		} else {
			log.Printf("复制本地海报失败 %s: %v", candidate, err)
		}
	}
	return "", nil
}

func localPosterCandidates(item map[string]any) []string {
	itemPathRaw, _ := item["Path"].(string)
	if strings.TrimSpace(itemPathRaw) == "" {
		return nil
	}
	itemPath := filepath.Clean(itemPathRaw)
	searchDirs := []string{filepath.Dir(itemPath)}
	info, err := os.Stat(itemPath)
	if err == nil && info.IsDir() {
		searchDirs = []string{itemPath}
	}
	seen := map[string]struct{}{}
	var out []string
	for _, dir := range searchDirs {
		if dir == "" {
			continue
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		for _, filename := range posterFilenames {
			candidate := filepath.Join(dir, filename)
			if fileExists(candidate) {
				out = append(out, candidate)
			}
		}
	}
	return out
}

func (s *Service) collectCandidateItems(ctx context.Context, client *emby.Client, library map[string]any, settings styleSettings, required int, sortBy string) ([]candidateItem, error) {
	itemTypes := resolveItemTypes(library)
	allowedTypes := allowedItemTypeSet(library)
	pageSize := maxInt(required*4, 50)
	maxScan := maxInt(required*30, 200)
	startIndex := 0
	uniqueCandidates := make([]candidateItem, 0)
	fallbackCandidates := make([]candidateItem, 0)
	seen := map[string]struct{}{}

	for startIndex < maxScan && len(uniqueCandidates) < required {
		items, err := client.GetLibraryItems(ctx, itemID(library), pageSize, sortBy, itemTypes, startIndex)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			itemType, _ := item["Type"].(string)
			itemType = strings.TrimSpace(itemType)
			if len(allowedTypes) > 0 {
				if _, ok := allowedTypes[itemType]; !ok {
					continue
				}
			}
			hasLocalPoster := len(localPosterCandidates(item)) > 0
			imgURL := client.GetImageURL(item, settings.UsePrimary)
			if !hasLocalPoster && strings.TrimSpace(imgURL) == "" {
				continue
			}
			candidate := candidateItem{item: item, imgURL: imgURL}
			fallbackCandidates = append(fallbackCandidates, candidate)
			logicalKey := logicalItemKey(item)
			if _, ok := seen[logicalKey]; ok {
				continue
			}
			seen[logicalKey] = struct{}{}
			uniqueCandidates = append(uniqueCandidates, candidate)
			if len(uniqueCandidates) >= required {
				break
			}
		}
		if len(items) < pageSize {
			break
		}
		startIndex += pageSize
	}

	if len(uniqueCandidates) > 0 {
		if len(uniqueCandidates) > required {
			uniqueCandidates = uniqueCandidates[:required]
		}
		return uniqueCandidates, nil
	}
	if len(fallbackCandidates) > required {
		fallbackCandidates = fallbackCandidates[:required]
	}
	return fallbackCandidates, nil
}

func resolveItemTypes(library map[string]any) string {
	collectionType := strings.ToLower(strings.TrimSpace(asString(library["CollectionType"])))
	switch collectionType {
	case "movies":
		return "Movie"
	case "tvshows":
		return "Series"
	case "music":
		return "MusicAlbum,Audio"
	case "":
		return "Movie,Series"
	default:
		return ""
	}
}

func allowedItemTypeSet(library map[string]any) map[string]struct{} {
	itemTypes := resolveItemTypes(library)
	if itemTypes == "" {
		return nil
	}
	out := map[string]struct{}{}
	for _, itemType := range strings.Split(itemTypes, ",") {
		itemType = strings.TrimSpace(itemType)
		if itemType != "" {
			out[itemType] = struct{}{}
		}
	}
	return out
}

func logicalItemKey(item map[string]any) string {
	itemType := strings.TrimSpace(asString(item["Type"]))
	if (itemType == "Episode" || itemType == "Season") && asString(item["SeriesId"]) != "" {
		return "series:" + asString(item["SeriesId"])
	}
	if primaryRefID := asString(item["PrimaryImageItemId"]); primaryRefID != "" {
		if primaryTag := asString(item["PrimaryImageTag"]); primaryTag != "" {
			return "primary:" + primaryRefID + ":" + primaryTag
		}
	}
	if providerIDs, ok := item["ProviderIds"].(map[string]any); ok {
		for _, provider := range []string{"Tmdb", "Imdb", "Tvdb"} {
			if value := strings.TrimSpace(asString(providerIDs[provider])); value != "" {
				return strings.ToLower(provider) + ":" + value
			}
		}
	}
	if id := strings.TrimSpace(asString(item["Id"])); id != "" {
		return "item:" + id
	}
	return "name:" + strings.ToLower(strings.TrimSpace(asString(item["Name"])))
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

func countValue(v any) int {
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
	case string:
		var n int
		_, _ = fmt.Sscanf(strings.TrimSpace(t), "%d", &n)
		return n
	default:
		return 0
	}
}

func copyFile(src, dst string) error {
	if src == "" || dst == "" {
		return fmt.Errorf("empty path")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
