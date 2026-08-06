package cover

import (
	"context"
	"path/filepath"
	"testing"

	"embytool/internal/config"
	"embytool/internal/fonts"
)

func TestGenerateSingleStyles(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		ZhFontPath: filepath.Join(root, "fonts", "zh_font.ttf"),
		EnFontPath: filepath.Join(root, "fonts", "en_font_multi_1.otf"),
	}
	svc := NewService(fonts.NewCache())

	for _, style := range []string{"single_1", "single_2"} {
		style := style
		t.Run(style, func(t *testing.T) {
			imageBytes, err := svc.GenerateManual(
				context.Background(),
				filepath.Join(root, "images", "1.jpg"),
				cfg,
				ManualParams{
					TitleZh:    "电影",
					TitleEn:    "Movies",
					CoverStyle: style,
					BlurSize:   50,
					ColorRatio: 0.8,
					ZhFontSize: 1.0,
					EnFontSize: 1.0,
				},
			)
			if err != nil {
				t.Fatalf("%s 生成失败: %v", style, err)
			}
			if len(imageBytes) == 0 {
				t.Fatalf("%s 生成了空图片", style)
			}
		})
	}
}
