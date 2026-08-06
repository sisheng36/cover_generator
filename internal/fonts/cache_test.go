package fonts

import (
	"path/filepath"
	"testing"
)

func TestBundledFontsLoad(t *testing.T) {
	cache := NewCache()

	fonts := []string{
		filepath.Join("..", "..", "fonts", "zh_font.ttf"),
		filepath.Join("..", "..", "fonts", "en_font_multi_1.otf"),
		filepath.Join("..", "..", "fonts", "zh_font_multi_1.ttf"),
	}

	for _, fontPath := range fonts {
		fontPath := fontPath
		t.Run(filepath.Base(fontPath), func(t *testing.T) {
			if _, err := cache.LoadFace(fontPath, 48); err != nil {
				t.Fatalf("LoadFace(%q) 失败: %v", fontPath, err)
			}
		})
	}
}
