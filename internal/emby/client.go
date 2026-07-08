package emby

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/draw"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

const itemFields = "Id,Name,Type,Path,ProviderIds,ImageTags,BackdropImageTags,PrimaryImageTag,PrimaryImageItemId,ParentBackdropImageTags,ParentBackdropItemId,SeriesPrimaryImageTag,SeriesId,SeriesName,ProductionYear,Overview,ChildCount,RecursiveItemCount"

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(serverURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(serverURL), "/"),
		apiKey:  strings.TrimSpace(apiKey),
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) do(ctx context.Context, method, path string, params url.Values, body io.Reader, contentType string) (*http.Response, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("empty emby server url")
	}
	u := c.baseURL + path
	if len(params) > 0 {
		if strings.Contains(u, "?") {
			u += "&" + params.Encode()
		} else {
			u += "?" + params.Encode()
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Emby-Token", c.apiKey)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.http.Do(req)
}

func (c *Client) getJSON(ctx context.Context, path string, params url.Values, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, params, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("emby GET %s -> %s", path, resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) GetUserID(ctx context.Context) (string, error) {
	var users []map[string]any
	if err := c.getJSON(ctx, "/Users", nil, &users); err != nil {
		log.Printf("Emby GET /Users 失败: %v", err)
		return "", nil
	}
	if len(users) == 0 {
		return "", nil
	}
	for _, user := range users {
		if policy, ok := user["Policy"].(map[string]any); ok {
			if admin, ok := policy["IsAdministrator"].(bool); ok && admin {
				if id, ok := user["Id"].(string); ok && id != "" {
					return id, nil
				}
			}
		}
	}
	if id, ok := users[0]["Id"].(string); ok && id != "" {
		return id, nil
	}
	return "", nil
}

func (c *Client) GetLibraries(ctx context.Context) ([]map[string]any, error) {
	uid, err := c.GetUserID(ctx)
	if err != nil {
		return []map[string]any{}, nil
	}
	if strings.TrimSpace(uid) == "" {
		return []map[string]any{}, nil
	}
	var resp struct {
		Items []map[string]any `json:"Items"`
	}
	if err := c.getJSON(ctx, "/Users/"+url.PathEscape(uid)+"/Views", nil, &resp); err != nil {
		log.Printf("Emby GET /Users/%s/Views 失败: %v", uid, err)
		return []map[string]any{}, nil
	}
	return resp.Items, nil
}

func (c *Client) GetLibraryItems(ctx context.Context, libraryID string, limit int, sortBy string, itemTypes string, startIndex int) ([]map[string]any, error) {
	uid, err := c.GetUserID(ctx)
	if err != nil {
		return []map[string]any{}, nil
	}
	if strings.TrimSpace(uid) == "" {
		return []map[string]any{}, nil
	}
	params := url.Values{}
	params.Set("ParentId", libraryID)
	params.Set("Limit", fmt.Sprintf("%d", limit))
	params.Set("SortBy", sortBy)
	params.Set("Fields", itemFields)
	params.Set("Recursive", "true")
	params.Set("StartIndex", fmt.Sprintf("%d", startIndex))
	if sortBy != "Random" {
		params.Set("SortOrder", "Descending")
	}
	if itemTypes != "" {
		params.Set("IncludeItemTypes", itemTypes)
	}

	var resp struct {
		Items []map[string]any `json:"Items"`
	}
	if err := c.getJSON(ctx, "/Users/"+url.PathEscape(uid)+"/Items", params, &resp); err != nil {
		log.Printf("Emby GET /Users/%s/Items 失败: %v", uid, err)
		return []map[string]any{}, nil
	}
	return resp.Items, nil
}

func (c *Client) GetItem(ctx context.Context, itemID string) (map[string]any, error) {
	if strings.TrimSpace(itemID) == "" {
		return nil, nil
	}
	var item map[string]any
	params := url.Values{}
	params.Set("Fields", itemFields)
	if err := c.getJSON(ctx, "/Items/"+url.PathEscape(itemID), params, &item); err != nil {
		log.Printf("Emby GET /Items/%s 失败: %v", itemID, err)
		return nil, nil
	}
	return item, nil
}

func strMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func strSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func (c *Client) GetImageURL(item map[string]any, usePrimary bool) string {
	itemID, _ := item["Id"].(string)
	if itemID == "" {
		return ""
	}

	var primaryURL, backdropURL, parentBackdropURL, seriesPrimaryURL string

	if tags, ok := strMap(item["ImageTags"])["Primary"].(string); ok && tags != "" {
		primaryURL = fmt.Sprintf("/emby/Items/%s/Images/Primary?tag=%s", url.PathEscape(itemID), url.QueryEscape(tags))
	} else {
		refID, _ := item["PrimaryImageItemId"].(string)
		refTag, _ := item["PrimaryImageTag"].(string)
		if refID != "" && refTag != "" {
			primaryURL = fmt.Sprintf("/emby/Items/%s/Images/Primary?tag=%s", url.PathEscape(refID), url.QueryEscape(refTag))
		}
	}

	if tags := strSlice(item["BackdropImageTags"]); len(tags) > 0 {
		backdropURL = fmt.Sprintf("/emby/Items/%s/Images/Backdrop/0?tag=%s", url.PathEscape(itemID), url.QueryEscape(tags[0]))
	}

	if tags := strSlice(item["ParentBackdropImageTags"]); len(tags) > 0 {
		parentID, _ := item["ParentBackdropItemId"].(string)
		if parentID != "" {
			parentBackdropURL = fmt.Sprintf("/emby/Items/%s/Images/Backdrop/0?tag=%s", url.PathEscape(parentID), url.QueryEscape(tags[0]))
		}
	}

	if tag, _ := item["SeriesPrimaryImageTag"].(string); tag != "" {
		if seriesID, _ := item["SeriesId"].(string); seriesID != "" {
			seriesPrimaryURL = fmt.Sprintf("/emby/Items/%s/Images/Primary?tag=%s", url.PathEscape(seriesID), url.QueryEscape(tag))
		}
	}

	itemType, _ := item["Type"].(string)
	if itemType == "Episode" {
		if usePrimary {
			return firstNonEmpty(seriesPrimaryURL, primaryURL, parentBackdropURL, backdropURL)
		}
		return firstNonEmpty(parentBackdropURL, backdropURL, seriesPrimaryURL, primaryURL)
	}

	if usePrimary {
		return firstNonEmpty(primaryURL, parentBackdropURL, backdropURL)
	}
	return firstNonEmpty(parentBackdropURL, backdropURL, primaryURL)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (c *Client) DownloadImage(ctx context.Context, apiPath, savePath string) (string, error) {
	if strings.TrimSpace(apiPath) == "" {
		return "", fmt.Errorf("empty api path")
	}
	resp, err := c.do(ctx, http.MethodGet, apiPath, nil, nil, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s -> %s", apiPath, resp.Status)
	}

	if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(savePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}
	return savePath, nil
}

func decodeImage(data []byte) (image.Image, string, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	if cfg.Width > 0 && cfg.Height > 0 && int64(cfg.Width)*int64(cfg.Height) > 89_000_000 {
		return nil, "", fmt.Errorf("image too large: %dx%d", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, format, err
}

func toJPEGBytes(data []byte) ([]byte, error) {
	img, _, err := decodeImage(data)
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	// Draw the source onto an opaque black canvas to match Pillow's RGB conversion.
	opaque := image.NewUniform(color.Black)
	draw.Draw(rgba, bounds, opaque, image.Point{}, draw.Src)
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Over)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: 95}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *Client) UploadLibraryImage(ctx context.Context, libraryID string, imageData []byte) (bool, error) {
	if strings.TrimSpace(libraryID) == "" {
		return false, fmt.Errorf("empty library id")
	}
	jpegBytes, err := toJPEGBytes(imageData)
	if err != nil {
		return false, err
	}

	body := base64.StdEncoding.EncodeToString(jpegBytes)
	resp, err := c.do(ctx, http.MethodPost, "/Items/"+url.PathEscape(libraryID)+"/Images/Primary", nil, strings.NewReader(body), "image/jpeg")
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return true, nil
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	return false, fmt.Errorf("upload library image failed: %s: %s", resp.Status, strings.TrimSpace(string(data)))
}
