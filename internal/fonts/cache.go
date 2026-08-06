package fonts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

type Cache struct {
	mu      sync.RWMutex
	assets  map[string]*asset
	faces   map[string]struct{}
	loadErr map[string]error
}

type asset struct {
	collection *opentype.Collection
	font       *opentype.Font
}

func NewCache() *Cache {
	return &Cache{
		assets:  map[string]*asset{},
		faces:   map[string]struct{}{},
		loadErr: map[string]error{},
	}
}

func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.faces)
}

func (c *Cache) LoadFace(path string, size float64) (font.Face, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("empty font path")
	}
	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	}

	asset, err := c.loadAsset(absPath)
	if err != nil {
		return nil, err
	}

	if asset.collection != nil {
		font0, err := asset.collection.Font(0)
		if err == nil {
			face, err := opentype.NewFace(font0, &opentype.FaceOptions{
				Size:    size,
				DPI:     72,
				Hinting: font.HintingFull,
			})
			if err == nil {
				c.recordFace(absPath, size)
			}
			return face, err
		}
		if asset.font == nil {
			return nil, err
		}
	}

	if asset.font == nil {
		return nil, fmt.Errorf("unsupported font format")
	}

	face, err := opentype.NewFace(asset.font, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	c.recordFace(absPath, size)
	return face, nil
}

func (c *Cache) recordFace(path string, size float64) {
	key := fmt.Sprintf("%s@%d", path, int(size))
	c.mu.Lock()
	c.faces[key] = struct{}{}
	c.mu.Unlock()
}

func (c *Cache) loadAsset(path string) (*asset, error) {
	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	}

	c.mu.RLock()
	if a, ok := c.assets[absPath]; ok {
		c.mu.RUnlock()
		return a, nil
	}
	if err, ok := c.loadErr[absPath]; ok {
		c.mu.RUnlock()
		return nil, err
	}
	c.mu.RUnlock()

	data, err := os.ReadFile(absPath)
	if err != nil {
		c.mu.Lock()
		c.loadErr[absPath] = err
		c.mu.Unlock()
		return nil, err
	}

	collection, err := opentype.ParseCollection(data)
	if err == nil {
		a := &asset{collection: collection}
		c.mu.Lock()
		c.assets[absPath] = a
		c.mu.Unlock()
		return a, nil
	}

	font0, fontErr := opentype.Parse(data)
	if fontErr != nil {
		fontErr = fmt.Errorf("加载字体 %s: %w", absPath, fontErr)
		c.mu.Lock()
		c.loadErr[absPath] = fontErr
		c.mu.Unlock()
		return nil, fontErr
	}

	a := &asset{font: font0}
	c.mu.Lock()
	c.assets[absPath] = a
	c.mu.Unlock()
	return a, nil
}
