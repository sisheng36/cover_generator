package cover

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/disintegration/imaging"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/vector"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	"embytool/internal/fonts"
)

var sanitizeNameRe = regexp.MustCompile(`[\\/:*?"<>|]+`)

type pointF struct {
	X float64
	Y float64
}

type textMetrics struct {
	Width  int
	Height int
	Ascent int
}

func sanitizeName(name string) string {
	safe := sanitizeNameRe.ReplaceAllString(name, "_")
	safe = strings.TrimSpace(safe)
	safe = strings.Trim(safe, ".")
	if safe == "" {
		return "library"
	}
	return safe
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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func toNRGBA(img image.Image) *image.NRGBA {
	if img == nil {
		return image.NewNRGBA(image.Rect(0, 0, 0, 0))
	}
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	return imaging.Clone(img)
}

func cloneImage(img image.Image) *image.NRGBA {
	return imaging.Clone(img)
}

func loadImageFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, err
	}
	if cfg.Width > 0 && cfg.Height > 0 && int64(cfg.Width)*int64(cfg.Height) > 89_000_000 {
		return nil, fmt.Errorf("image too large: %dx%d", cfg.Width, cfg.Height)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	_ = format
	return imaging.Decode(f)
}

func loadNRGBA(path string) (*image.NRGBA, error) {
	img, err := loadImageFile(path)
	if err != nil {
		return nil, err
	}
	return toNRGBA(img), nil
}

func imageToPNGBytes(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func savePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

func blendWithColor(imageSrc image.Image, c color.NRGBA, ratio float64) *image.NRGBA {
	base := toNRGBA(imageSrc)
	ratio = clamp01(ratio)
	if ratio <= 0 {
		return imaging.Clone(base)
	}
	overlay := imaging.New(base.Bounds().Dx(), base.Bounds().Dy(), c)
	return imaging.Overlay(base, overlay, image.Point{}, ratio)
}

func addFilmGrain(imageSrc image.Image, intensity float64) *image.NRGBA {
	base := toNRGBA(imageSrc)
	strength := math.Max(0, intensity)
	if strength <= 0 {
		return imaging.Clone(base)
	}

	smallW := maxInt(64, base.Bounds().Dx()/4)
	smallH := maxInt(64, base.Bounds().Dy()/4)
	noise := image.NewGray(image.Rect(0, 0, smallW, smallH))
	for y := 0; y < smallH; y++ {
		for x := 0; x < smallW; x++ {
			noise.SetGray(x, y, color.Gray{Y: uint8(rand.Intn(255))})
		}
	}
	noiseBig := imaging.Resize(noise, base.Bounds().Dx(), base.Bounds().Dy(), imaging.NearestNeighbor)
	return imaging.Overlay(base, noiseBig, image.Point{}, math.Min(0.18, strength*0.9))
}

func createHorizontalGradientMask(size image.Point, power float64) *image.Alpha {
	width, height := size.X, size.Y
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	if width <= 0 || height <= 0 {
		return mask
	}
	if power <= 0 {
		power = 1
	}
	for x := 0; x < width; x++ {
		var value float64
		if width == 1 {
			value = 1
		} else {
			value = math.Pow(float64(x)/float64(width-1), power)
		}
		alpha := uint8(clamp01(value) * 255)
		for y := 0; y < height; y++ {
			mask.SetAlpha(x, y, color.Alpha{A: alpha})
		}
	}
	return mask
}

func rgbToHSV(c color.NRGBA) (float64, float64, float64) {
	r := float64(c.R) / 255.0
	g := float64(c.G) / 255.0
	b := float64(c.B) / 255.0
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	v := max
	d := max - min
	if d == 0 {
		return 0, 0, v
	}
	s := d / max
	var h float64
	switch max {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h /= 6
	if h < 0 {
		h += 1
	}
	return h, s, v
}

func hsvToRGB(h, s, v float64) color.NRGBA {
	if s == 0 {
		gray := uint8(clamp01(v) * 255)
		return color.NRGBA{R: gray, G: gray, B: gray, A: 255}
	}
	h = math.Mod(h, 1)
	if h < 0 {
		h += 1
	}
	i := int(h * 6)
	f := h*6 - float64(i)
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)
	var r, g, b float64
	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	case 5:
		r, g, b = v, p, q
	}
	return color.NRGBA{
		R: uint8(clamp01(r) * 255),
		G: uint8(clamp01(g) * 255),
		B: uint8(clamp01(b) * 255),
		A: 255,
	}
}

func isNotBlackWhiteGrayNear(c color.NRGBA, threshold int) bool {
	r, g, b := int(c.R), int(c.G), int(c.B)
	if (r < threshold && g < threshold && b < threshold) || (r > 255-threshold && g > 255-threshold && b > 255-threshold) {
		return false
	}
	const grayDiffThreshold = 10
	if absInt(r-g) < grayDiffThreshold && absInt(g-b) < grayDiffThreshold && absInt(r-b) < grayDiffThreshold {
		return false
	}
	return true
}

func adjustColorMacaron(c color.NRGBA) color.NRGBA {
	h, s, v := rgbToHSV(c)
	if s < 0.3 {
		s = 0.3
	} else if s > 0.7 {
		s = 0.7
	}
	if v < 0.6 {
		v = 0.6
	} else if v > 0.85 {
		v = 0.85
	}
	return hsvToRGB(h, s, v)
}

func colorDistance(c1, c2 color.NRGBA) float64 {
	h1, s1, v1 := rgbToHSV(c1)
	h2, s2, v2 := rgbToHSV(c2)
	hDist := math.Min(math.Abs(h1-h2), 1-math.Abs(h1-h2))
	return hDist*5 + math.Abs(s1-s2) + math.Abs(v1-v2)
}

func findDominantMacaronColors(img image.Image, numColors int) []color.NRGBA {
	small := imaging.Resize(img, 150, 150, imaging.Lanczos)
	bounds := small.Bounds()
	counts := map[color.NRGBA]int{}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(small.At(x, y)).(color.NRGBA)
			if !isNotBlackWhiteGrayNear(c, 20) {
				continue
			}
			c.A = 255
			counts[c]++
		}
	}
	type pair struct {
		c color.NRGBA
		n int
	}
	pairs := make([]pair, 0, len(counts))
	for c, n := range counts {
		pairs = append(pairs, pair{c: c, n: n})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].n > pairs[j].n })
	candidates := make([]color.NRGBA, 0, numColors)
	for _, p := range pairs[:minInt(len(pairs), numColors*5)] {
		adjusted := adjustColorMacaron(p.c)
		similar := false
		for _, existing := range candidates {
			if colorDistance(adjusted, existing) < 0.15 {
				similar = true
				break
			}
		}
		if similar {
			continue
		}
		candidates = append(candidates, adjusted)
		if len(candidates) >= numColors {
			break
		}
	}
	return candidates
}

func findDominantVibrantColors(img image.Image, numColors int) []color.NRGBA {
	small := imaging.Resize(img, 100, 100, imaging.Lanczos)
	bounds := small.Bounds()
	counts := map[color.NRGBA]int{}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(small.At(x, y)).(color.NRGBA)
			if !isNotBlackWhiteGrayNear(c, 20) {
				continue
			}
			c.A = 255
			counts[c]++
		}
	}
	type pair struct {
		c color.NRGBA
		n int
	}
	pairs := make([]pair, 0, len(counts))
	for c, n := range counts {
		pairs = append(pairs, pair{c: c, n: n})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].n > pairs[j].n })
	candidates := make([]color.NRGBA, 0, numColors)
	seenHue := map[int]struct{}{}
	for _, p := range pairs[:minInt(len(pairs), numColors*3)] {
		h, s, v := rgbToHSV(p.c)
		if s < 0.2 {
			s = 0.2
		} else if s > 0.7 {
			s = 0.7
		}
		if v < 0.55 {
			v = 0.55
		} else if v > 0.85 {
			v = 0.85
		}
		adjusted := hsvToRGB(h, s, v)
		hueDegree := int(h * 360)
		similar := false
		for seen := range seenHue {
			if absInt(hueDegree-seen) < 15 {
				similar = true
				break
			}
		}
		if similar {
			continue
		}
		already := false
		for _, existing := range candidates {
			if existing == adjusted {
				already = true
				break
			}
		}
		if already {
			continue
		}
		candidates = append(candidates, adjusted)
		seenHue[hueDegree] = struct{}{}
		if len(candidates) >= numColors {
			break
		}
	}
	return candidates
}

func darkenColor(c color.NRGBA, factor float64) color.NRGBA {
	if factor <= 0 {
		return color.NRGBA{A: c.A}
	}
	if factor > 1 {
		factor = 1
	}
	return color.NRGBA{
		R: uint8(float64(c.R) * factor),
		G: uint8(float64(c.G) * factor),
		B: uint8(float64(c.B) * factor),
		A: c.A,
	}
}

func cropToSquare(img image.Image) image.Image {
	b := img.Bounds()
	size := minInt(b.Dx(), b.Dy())
	left := b.Min.X + (b.Dx()-size)/2
	top := b.Min.Y + (b.Dy()-size)/2
	return imaging.Crop(img, image.Rect(left, top, left+size, top+size))
}

func roundedRectMask(width, height, radius int) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	if width <= 0 || height <= 0 {
		return mask
	}
	if radius <= 0 {
		draw.Draw(mask, mask.Bounds(), image.Opaque, image.Point{}, draw.Src)
		return mask
	}
	if radius*2 > width {
		radius = width / 2
	}
	if radius*2 > height {
		radius = height / 2
	}
	const steps = 16
	points := make([]pointF, 0, steps*4+4)
	cx, cy := float64(radius), float64(radius)
	// Top-left arc.
	for i := 0; i <= steps; i++ {
		theta := math.Pi + (math.Pi/2)*float64(i)/float64(steps)
		points = append(points, pointF{X: cx + float64(radius)*math.Cos(theta), Y: cy + float64(radius)*math.Sin(theta)})
	}
	// Top-right arc.
	cx = float64(width - radius)
	cy = float64(radius)
	for i := 0; i <= steps; i++ {
		theta := -math.Pi / 2 * float64(i) / float64(steps)
		points = append(points, pointF{X: cx + float64(radius)*math.Cos(theta), Y: cy + float64(radius)*math.Sin(theta)})
	}
	// Bottom-right arc.
	cx = float64(width - radius)
	cy = float64(height - radius)
	for i := 0; i <= steps; i++ {
		theta := float64(i) * (math.Pi / 2) / float64(steps)
		points = append(points, pointF{X: cx + float64(radius)*math.Cos(theta), Y: cy + float64(radius)*math.Sin(theta)})
	}
	// Bottom-left arc.
	cx = float64(radius)
	cy = float64(height - radius)
	for i := 0; i <= steps; i++ {
		theta := math.Pi / 2 + float64(i)*(math.Pi/2)/float64(steps)
		points = append(points, pointF{X: cx + float64(radius)*math.Cos(theta), Y: cy + float64(radius)*math.Sin(theta)})
	}

	return polygonMask(width, height, points)
}

func polygonMask(width, height int, points []pointF) *image.Alpha {
	mask := image.NewAlpha(image.Rect(0, 0, width, height))
	if len(points) < 3 || width <= 0 || height <= 0 {
		return mask
	}
	r := vector.NewRasterizer(width, height)
	r.MoveTo(float32(points[0].X), float32(points[0].Y))
	for _, p := range points[1:] {
		r.LineTo(float32(p.X), float32(p.Y))
	}
	r.LineTo(float32(points[0].X), float32(points[0].Y))
	r.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	return mask
}

func addRoundedCorners(img image.Image, radius int) *image.NRGBA {
	base := toNRGBA(img)
	mask := roundedRectMask(base.Bounds().Dx(), base.Bounds().Dy(), radius)
	dst := image.NewNRGBA(base.Bounds())
	draw.DrawMask(dst, dst.Bounds(), base, image.Point{}, mask, image.Point{}, draw.Src)
	return dst
}

func addShadowAndRotate(canvas *image.NRGBA, img image.Image, angle float64, offset image.Point, radius int, opacity float64, centerPos image.Point) *image.NRGBA {
	base := toNRGBA(img)
	if canvas == nil {
		canvas = image.NewNRGBA(image.Rect(0, 0, 0, 0))
	}
	if centerPos == (image.Point{}) {
		centerPos = image.Pt(canvas.Bounds().Dx()/2, canvas.Bounds().Dy()/2)
	}
	padding := maxInt(radius*4, 100)
	shadowSize := image.Pt(base.Bounds().Dx()+padding*2, base.Bounds().Dy()+padding*2)
	shadow := image.NewNRGBA(image.Rect(0, 0, shadowSize.X, shadowSize.Y))
	shadowMask := image.NewAlpha(base.Bounds())
	draw.Draw(shadowMask, shadowMask.Bounds(), base, image.Point{}, draw.Src)
	shadowCenter := image.Pt(padding, padding)
	shadowColor := color.NRGBA{0, 0, 0, uint8(clamp01(opacity) * 255)}
	shadowLayer := imaging.New(base.Bounds().Dx(), base.Bounds().Dy(), shadowColor)
	draw.DrawMask(shadow, image.Rectangle{Min: shadowCenter, Max: shadowCenter.Add(base.Bounds().Size())}, shadowLayer, image.Point{}, shadowMask, image.Point{}, draw.Over)
	shadow = imaging.Blur(shadow, float64(radius))
	rotatedShadow := rotateBicubic(shadow, angle)
	shadowX := centerPos.X - rotatedShadow.Bounds().Dx()/2 + offset.X
	shadowY := centerPos.Y - rotatedShadow.Bounds().Dy()/2 + offset.Y
	draw.Draw(canvas, image.Rect(shadowX, shadowY, shadowX+rotatedShadow.Bounds().Dx(), shadowY+rotatedShadow.Bounds().Dy()), rotatedShadow, image.Point{}, draw.Over)

	rotatedImg := rotateBicubic(base, angle)
	imgX := centerPos.X - rotatedImg.Bounds().Dx()/2
	imgY := centerPos.Y - rotatedImg.Bounds().Dy()/2
	draw.Draw(canvas, image.Rect(imgX, imgY, imgX+rotatedImg.Bounds().Dx(), imgY+rotatedImg.Bounds().Dy()), rotatedImg, image.Point{}, draw.Over)
	return canvas
}

func createShadowLayer(img image.Image, offset image.Point, shadowColor color.NRGBA, blurRadius int) *image.NRGBA {
	base := toNRGBA(img)
	shadow := image.NewNRGBA(base.Bounds())
	layer := imaging.New(base.Bounds().Dx(), base.Bounds().Dy(), shadowColor)
	mask := image.NewAlpha(base.Bounds())
	draw.Draw(mask, mask.Bounds(), base, image.Point{}, draw.Src)
	draw.DrawMask(shadow, image.Rect(offset.X, offset.Y, offset.X+base.Bounds().Dx(), offset.Y+base.Bounds().Dy()), layer, image.Point{}, mask, image.Point{}, draw.Over)
	return imaging.Blur(shadow, float64(blurRadius))
}

func drawString(dst *image.NRGBA, text string, x, y float64, face font.Face, fill color.NRGBA) {
	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	d := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(fill),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.Int26_6(math.Round(x * 64)), Y: fixed.Int26_6(math.Round((y+float64(ascent))*64))},
	}
	d.DrawString(text)
}

func measureText(face font.Face, text string) textMetrics {
	d := &font.Drawer{Face: face}
	width := d.MeasureString(text).Ceil()
	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	height := metrics.Ascent.Ceil() + metrics.Descent.Ceil()
	return textMetrics{Width: width, Height: height, Ascent: ascent}
}

func drawTextOnImage(base image.Image, text string, position pointF, fontCache *fonts.Cache, fontPath string, fontSize int, fillColor color.NRGBA, shadow bool, shadowColor color.NRGBA, shadowOffset int, shadowAlpha uint8) (*image.NRGBA, error) {
	canvas := toNRGBA(base)
	face, err := fontCache.LoadFace(fontPath, float64(fontSize))
	if err != nil {
		return nil, err
	}
	defer closeFace(face)

	textLayer := image.NewNRGBA(canvas.Bounds())
	shadowLayer := image.NewNRGBA(canvas.Bounds())
	if shadow {
		if shadowColor.A == 0 {
			shadowColor = color.NRGBA{R: uint8(float64(fillColor.R) * 0.7), G: uint8(float64(fillColor.G) * 0.7), B: uint8(float64(fillColor.B) * 0.7), A: shadowAlpha}
		} else {
			shadowColor.A = shadowAlpha
		}
		for offset := 3; offset <= shadowOffset; offset += 2 {
			drawString(shadowLayer, text, position.X+float64(offset), position.Y+float64(offset), face, shadowColor)
		}
		shadowLayer = imaging.Blur(shadowLayer, float64(shadowOffset))
		draw.Draw(canvas, canvas.Bounds(), shadowLayer, image.Point{}, draw.Over)
	}

	drawString(textLayer, text, position.X, position.Y, face, fillColor)
	draw.Draw(canvas, canvas.Bounds(), textLayer, image.Point{}, draw.Over)
	return canvas, nil
}

func drawMultilineTextOnImage(base image.Image, text string, position pointF, fontCache *fonts.Cache, fontPath string, fontSize int, lineSpacing float64, fillColor color.NRGBA, shadow bool, shadowColor color.NRGBA, shadowOffset int, shadowAlpha uint8) (*image.NRGBA, int, error) {
	canvas := toNRGBA(base)
	face, err := fontCache.LoadFace(fontPath, float64(fontSize))
	if err != nil {
		return nil, 0, err
	}
	defer closeFace(face)

	words := strings.Fields(text)
	if len(words) <= 1 {
		result, err := drawTextOnImage(canvas, text, position, fontCache, fontPath, fontSize, fillColor, shadow, shadowColor, shadowOffset, shadowAlpha)
		if err != nil {
			return nil, 0, err
		}
		return result, 1, nil
	}

	textLayer := image.NewNRGBA(canvas.Bounds())
	shadowLayer := image.NewNRGBA(canvas.Bounds())
	if shadow {
		if shadowColor.A == 0 {
			shadowColor = color.NRGBA{R: uint8(float64(fillColor.R) * 0.7), G: uint8(float64(fillColor.G) * 0.7), B: uint8(float64(fillColor.B) * 0.7), A: shadowAlpha}
		} else {
			shadowColor.A = shadowAlpha
		}
	}
	for i, word := range words {
		y := position.Y + float64(i)*(float64(fontSize)+lineSpacing)
		if shadow {
			for offset := 3; offset <= shadowOffset; offset += 2 {
				drawString(shadowLayer, word, position.X+float64(offset), y+float64(offset), face, shadowColor)
			}
		}
		drawString(textLayer, word, position.X, y, face, fillColor)
	}
	if shadow {
		shadowLayer = imaging.Blur(shadowLayer, float64(shadowOffset))
		draw.Draw(canvas, canvas.Bounds(), shadowLayer, image.Point{}, draw.Over)
	}
	draw.Draw(canvas, canvas.Bounds(), textLayer, image.Point{}, draw.Over)
	return canvas, len(words), nil
}

func getRandomColor(path string) color.NRGBA {
	img, err := loadImageFile(path)
	if err != nil {
		return color.NRGBA{R: uint8(50 + rand.Intn(151)), G: uint8(50 + rand.Intn(151)), B: uint8(50 + rand.Intn(151)), A: 255}
	}
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return color.NRGBA{R: 120, G: 120, B: 120, A: 255}
	}
	x := rand.Intn(maxInt(1, int(float64(b.Dx())*0.3))) + int(float64(b.Dx())*0.5)
	y := rand.Intn(maxInt(1, int(float64(b.Dy())*0.3))) + int(float64(b.Dy())*0.5)
	x = minInt(x, b.Max.X-1)
	y = minInt(y, b.Max.Y-1)
	c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	c.A = 255
	return c
}

func drawColorBlock(img image.Image, position pointF, size pointF, c color.NRGBA) *image.NRGBA {
	canvas := toNRGBA(img)
	rect := image.Rect(int(math.Round(position.X)), int(math.Round(position.Y)), int(math.Round(position.X+size.X)), int(math.Round(position.Y+size.Y)))
	draw.Draw(canvas, rect, imaging.New(rect.Dx(), rect.Dy(), c), image.Point{}, draw.Over)
	return canvas
}

func createGradientBackground(width, height int, colors []color.NRGBA) *image.NRGBA {
	var selected color.NRGBA
	hasSelected := false
	for _, c := range colors {
		if isMidBrightHSL(c, 0.3, 0.7) {
			selected = c
			hasSelected = true
			break
		}
	}
	if !hasSelected {
		h := rand.Float64()
		l := 0.5 + rand.Float64()*0.3
		s := 0.5 + rand.Float64()*0.5
		selected = hlsToRGB(h, l, s)
	}
	selected = color.NRGBA{R: scaleUint8(selected.R, 0.65), G: scaleUint8(selected.G, 0.65), B: scaleUint8(selected.B, 0.65), A: 255}
	left := imaging.New(width, height, selected)
	rightColor := color.NRGBA{R: scaleUint8(selected.R, 1.9), G: scaleUint8(selected.G, 1.9), B: scaleUint8(selected.B, 1.9), A: 255}
	right := imaging.New(width, height, rightColor)
	mask := createHorizontalGradientMask(image.Pt(width, height), 0.7)
	dst := imaging.New(width, height, selected)
	draw.DrawMask(dst, dst.Bounds(), right, image.Point{}, mask, image.Point{}, draw.Over)
	_ = left
	return dst
}

func alignImageRight(img image.Image, canvasSize image.Point) *image.NRGBA {
	canvasWidth, canvasHeight := canvasSize.X, canvasSize.Y
	targetWidth := int(float64(canvasWidth) * 0.675)
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return imaging.New(canvasWidth, canvasHeight, color.NRGBA{A: 255})
	}

	scaleFactor := float64(canvasHeight) / float64(b.Dy())
	newImgWidth := int(float64(b.Dx()) * scaleFactor)
	resized := imaging.Resize(img, newImgWidth, canvasHeight, imaging.Lanczos)
	if newImgWidth < targetWidth {
		scaleFactor = float64(targetWidth) / float64(b.Dx())
		newImgHeight := int(float64(b.Dy()) * scaleFactor)
		resized = imaging.Resize(img, targetWidth, newImgHeight, imaging.Lanczos)
		if newImgHeight > canvasHeight {
			cropTop := (newImgHeight - canvasHeight) / 2
			resized = imaging.Crop(resized, image.Rect(0, cropTop, targetWidth, cropTop+canvasHeight))
		}
		finalImg := imaging.New(canvasWidth, canvasHeight, color.NRGBA{A: 255})
		draw.Draw(finalImg, image.Rect(canvasWidth-targetWidth, 0, canvasWidth, canvasHeight), resized, image.Point{}, draw.Over)
		return finalImg
	}

	resizedCenterX := float64(newImgWidth) / 2
	cropLeft := int(math.Max(0, resizedCenterX-float64(targetWidth)/2))
	if cropLeft+targetWidth > newImgWidth {
		cropLeft = newImgWidth - targetWidth
	}
	if cropLeft < 0 {
		cropLeft = 0
	}
	cropRight := cropLeft + targetWidth
	if cropRight > newImgWidth {
		cropRight = newImgWidth
	}
	cropped := imaging.Crop(resized, image.Rect(cropLeft, 0, cropRight, canvasHeight))
	finalImg := imaging.New(canvasWidth, canvasHeight, color.NRGBA{A: 255})
	pasteX := canvasWidth - cropped.Bounds().Dx() + int(float64(canvasWidth)*0.075)
	draw.Draw(finalImg, image.Rect(pasteX, 0, pasteX+cropped.Bounds().Dx(), canvasHeight), cropped, image.Point{}, draw.Over)
	return finalImg
}

func drawBadge(img image.Image, itemCount int, fontCache *fonts.Cache, fontPath string, style string, sizeRatio float64, baseColor color.NRGBA) (*image.NRGBA, error) {
	if itemCount <= 0 {
		return toNRGBA(img), nil
	}

	canvas := toNRGBA(img)
	canvasHeight := canvas.Bounds().Dy()
	badgeFontSize := int(float64(canvasHeight) * sizeRatio)
	if badgeFontSize < 12 {
		badgeFontSize = 12
	}
	margin := int(float64(canvasHeight) * 0.04)
	countText := fmt.Sprintf("%d", itemCount)

	face, err := fontCache.LoadFace(fontPath, float64(badgeFontSize))
	if err != nil {
		face = basicfont.Face7x13
	} else {
		defer closeFace(face)
	}

	switch style {
	case "ribbon":
		ribbonWidth := int(float64(badgeFontSize) * 3.0)
		if ribbonWidth < badgeFontSize*2 {
			ribbonWidth = badgeFontSize * 2
		}
		foldSize := int(float64(ribbonWidth) * 0.5)
		ribbonFill := color.NRGBA{R: 250, G: 222, B: 135, A: 250}
		ribbonLayer := image.NewNRGBA(image.Rect(0, 0, ribbonWidth, ribbonWidth))
		ribbonMask := polygonMask(ribbonWidth, ribbonWidth, []pointF{
			{X: 0, Y: 0},
			{X: float64(ribbonWidth), Y: 0},
			{X: 0, Y: float64(ribbonWidth)},
		})
		draw.DrawMask(ribbonLayer, ribbonLayer.Bounds(), imaging.New(ribbonWidth, ribbonWidth, ribbonFill), image.Point{}, ribbonMask, image.Point{}, draw.Over)
		cutoutMask := polygonMask(ribbonWidth, ribbonWidth, []pointF{
			{X: 0, Y: 0},
			{X: float64(foldSize), Y: 0},
			{X: 0, Y: float64(foldSize)},
		})
		draw.DrawMask(ribbonLayer, ribbonLayer.Bounds(), imaging.New(ribbonWidth, ribbonWidth, color.NRGBA{A: 0}), image.Point{}, cutoutMask, image.Point{}, draw.Src)
		draw.Draw(canvas, image.Rect(0, 0, ribbonWidth, ribbonWidth), ribbonLayer, image.Point{}, draw.Over)

		textColor := color.NRGBA{R: 89, G: 52, B: 2, A: 245}
		textShadow := color.NRGBA{A: 80}
		textMetrics := measureText(face, countText)
		textCanvasSize := int(math.Sqrt(float64(textMetrics.Width*textMetrics.Width+textMetrics.Height*textMetrics.Height)) * 1.5)
		if textCanvasSize < ribbonWidth {
			textCanvasSize = ribbonWidth
		}
		textCanvas := image.NewNRGBA(image.Rect(0, 0, textCanvasSize, textCanvasSize))
		centerX := float64(textCanvasSize) / 2
		centerY := float64(textCanvasSize) / 2
		drawString(textCanvas, countText, centerX+2, centerY+2-float64(textMetrics.Ascent)/2, face, textShadow)
		drawString(textCanvas, countText, centerX, centerY-float64(textMetrics.Ascent)/2, face, textColor)
		rotatedText := rotateBicubic(textCanvas, 45)
		positionFactor := 0.38
		pasteCenterX := int(float64(ribbonWidth) * positionFactor)
		pasteCenterY := int(float64(ribbonWidth) * positionFactor)
		pasteX := pasteCenterX - rotatedText.Bounds().Dx()/2
		pasteY := pasteCenterY - rotatedText.Bounds().Dy()/2
		draw.Draw(canvas, image.Rect(pasteX, pasteY, pasteX+rotatedText.Bounds().Dx(), pasteY+rotatedText.Bounds().Dy()), rotatedText, image.Point{}, draw.Over)
		return canvas, nil

	default:
		textMetrics := measureText(face, countText)
		badgePaddingH := int(float64(badgeFontSize) * 0.4)
		badgePaddingV := int(float64(badgeFontSize) * 0.2)
		badgeWidth := textMetrics.Width + badgePaddingH*2
		badgeHeight := textMetrics.Height + badgePaddingV*2
		badgePos := image.Pt(margin, margin)
		badgeFill := color.NRGBA{R: 40, G: 40, B: 40, A: 180}
		if baseColor.A != 0 {
			dark := darkenColor(baseColor, 0.3)
			badgeFill = color.NRGBA{R: dark.R, G: dark.G, B: dark.B, A: 190}
		}
		badgeLayer := image.NewNRGBA(image.Rect(0, 0, badgeWidth, badgeHeight))
		badgeMask := roundedRectMask(badgeWidth, badgeHeight, maxInt(1, int(float64(badgeHeight)*0.3)))
		draw.DrawMask(badgeLayer, badgeLayer.Bounds(), imaging.New(badgeWidth, badgeHeight, badgeFill), image.Point{}, badgeMask, image.Point{}, draw.Over)
		draw.Draw(canvas, image.Rect(badgePos.X, badgePos.Y, badgePos.X+badgeWidth, badgePos.Y+badgeHeight), badgeLayer, image.Point{}, draw.Over)

		textX := float64(badgePos.X + (badgeWidth-textMetrics.Width)/2)
		textY := float64(badgePos.Y + (badgeHeight-textMetrics.Height)/2)
		drawString(canvas, countText, textX+2, textY+2, face, color.NRGBA{A: 100})
		drawString(canvas, countText, textX, textY, face, color.NRGBA{R: 255, G: 255, B: 255, A: 240})
		return canvas, nil
	}
}

func isMidBrightHSL(c color.NRGBA, minL, maxL float64) bool {
	r := float64(c.R) / 255
	g := float64(c.G) / 255
	b := float64(c.B) / 255
	_, l, _ := rgbToHSL(r, g, b)
	return l >= minL && l <= maxL
}

func rgbToHSL(r, g, b float64) (h, l, s float64) {
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	l = (max + min) / 2
	if max == min {
		return 0, l, 0
	}
	d := max - min
	if l > 0.5 {
		s = d / (2 - max - min)
	} else {
		s = d / (max + min)
	}
	switch max {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	case b:
		h = (r-g)/d + 4
	}
	h /= 6
	if h < 0 {
		h += 1
	}
	return h, l, s
}

func hlsToRGB(h, l, s float64) color.NRGBA {
	if s == 0 {
		v := uint8(clamp01(l) * 255)
		return color.NRGBA{R: v, G: v, B: v, A: 255}
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	r := hueToRGB(p, q, h+1.0/3.0)
	g := hueToRGB(p, q, h)
	b := hueToRGB(p, q, h-1.0/3.0)
	return color.NRGBA{R: uint8(clamp01(r) * 255), G: uint8(clamp01(g) * 255), B: uint8(clamp01(b) * 255), A: 255}
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	default:
		return p
	}
}

func createBlurBackground(imagePath string, width, height int, backgroundColor color.NRGBA, blurSize int, colorRatio float64, lightenGradientStrength float64) (*image.NRGBA, error) {
	img, err := loadImageFile(imagePath)
	if err != nil {
		return nil, err
	}
	bgImg := imaging.Fill(img, width, height, imaging.Center, imaging.Lanczos)
	bgImg = imaging.Blur(bgImg, float64(blurSize))
	actualColor := darkenColor(backgroundColor, 0.85)
	blended := blendWithColor(bgImg, actualColor, colorRatio)
	if lightenGradientStrength > 0 {
		maxAlpha := uint8(clamp01(lightenGradientStrength) * 255)
		mask := createHorizontalGradientMask(image.Pt(width, height), 1.0)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				a := uint8(float64(mask.AlphaAt(x, y).A) / 255 * float64(maxAlpha))
				mask.SetAlpha(x, y, color.Alpha{A: a})
			}
		}
		lighten := imaging.New(width, height, color.NRGBA{R: 255, G: 255, B: 255, A: 0})
		draw.DrawMask(lighten, lighten.Bounds(), imaging.New(width, height, color.NRGBA{R: 255, G: 255, B: 255, A: 255}), image.Point{}, mask, image.Point{}, draw.Over)
		blended = imaging.Overlay(blended, lighten, image.Point{}, 1)
	}
	return addFilmGrain(blended, 0.03), nil
}

func getPosterPrimaryColors(imagePath string) []color.NRGBA {
	img, err := loadImageFile(imagePath)
	if err != nil {
		return []color.NRGBA{{R: 150, G: 100, B: 50, A: 255}}
	}
	small := imaging.Resize(img, 100, 150, imaging.Lanczos)
	bounds := small.Bounds()
	counts := map[color.NRGBA]int{}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(small.At(x, y)).(color.NRGBA)
			if c.A > 200 && !(c.R < 30 && c.G < 30 && c.B < 30) && !(c.R > 220 && c.G > 220 && c.B > 220) {
				c.A = 255
				counts[c]++
			}
		}
	}
	if len(counts) == 0 {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := color.NRGBAModel.Convert(small.At(x, y)).(color.NRGBA)
				if c.A > 100 {
					c.A = 255
					counts[c]++
				}
			}
		}
	}
	type pair struct {
		c color.NRGBA
		n int
	}
	pairs := make([]pair, 0, len(counts))
	for c, n := range counts {
		pairs = append(pairs, pair{c: c, n: n})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].n > pairs[j].n })
	if len(pairs) == 0 {
		return []color.NRGBA{{R: 150, G: 100, B: 50, A: 255}}
	}
	out := make([]color.NRGBA, 0, minInt(len(pairs), 10))
	for _, p := range pairs[:minInt(len(pairs), 10)] {
		out = append(out, p.c)
	}
	return out
}

func addShadow(img image.Image, offset image.Point, shadowColor color.NRGBA, blurRadius int) *image.NRGBA {
	base := toNRGBA(img)
	shadowMask := image.NewAlpha(base.Bounds())
	draw.Draw(shadowMask, shadowMask.Bounds(), base, image.Point{}, draw.Over)
	shadowLayers := []struct {
		offX  int
		offY  int
		blur  int
		alpha uint8
	}{
		{
			offX:  maxInt(1, int(float64(offset.X)*0.45)),
			offY:  maxInt(1, int(float64(offset.Y)*0.45)),
			blur:  maxInt(2, int(float64(blurRadius)*0.42)),
			alpha: uint8(float64(shadowColor.A) * 0.62),
		},
		{
			offX:  offset.X,
			offY:  offset.Y,
			blur:  blurRadius,
			alpha: shadowColor.A,
		},
	}
	maxBlur := 0
	maxOffsetX := 0
	maxOffsetY := 0
	for _, layer := range shadowLayers {
		maxBlur = maxInt(maxBlur, layer.blur)
		maxOffsetX = maxInt(maxOffsetX, layer.offX)
		maxOffsetY = maxInt(maxOffsetY, layer.offY)
	}
	shadow := image.NewNRGBA(image.Rect(0, 0, base.Bounds().Dx()+maxOffsetX+maxBlur*2, base.Bounds().Dy()+maxOffsetY+maxBlur*2))
	for _, layer := range shadowLayers {
		layerImg := imaging.New(base.Bounds().Dx(), base.Bounds().Dy(), color.NRGBA{R: shadowColor.R, G: shadowColor.G, B: shadowColor.B, A: layer.alpha})
		layerCanvas := image.NewNRGBA(shadow.Bounds())
		draw.DrawMask(layerCanvas, image.Rect(maxBlur+layer.offX, maxBlur+layer.offY, maxBlur+layer.offX+base.Bounds().Dx(), maxBlur+layer.offY+base.Bounds().Dy()), layerImg, image.Point{}, shadowMask, image.Point{}, draw.Over)
		blurred := imaging.Blur(layerCanvas, float64(layer.blur))
		shadow = imaging.Overlay(shadow, blurred, image.Point{}, 1)
	}
	result := image.NewNRGBA(shadow.Bounds())
	draw.Draw(result, image.Rect(maxBlur, maxBlur, maxBlur+base.Bounds().Dx(), maxBlur+base.Bounds().Dy()), base, image.Point{}, draw.Over)
	return imaging.Overlay(shadow, result, image.Point{}, 1)
}

func createDiagonalMask(size image.Point, splitTop, splitBottom float64) *image.Alpha {
	width, height := size.X, size.Y
	points := []pointF{
		{X: float64(int(float64(width) * splitTop)), Y: 0},
		{X: float64(width), Y: 0},
		{X: float64(width), Y: float64(height)},
		{X: float64(int(float64(width) * splitBottom)), Y: float64(height)},
	}
	return polygonMask(width, height, points)
}

func createShadowMask(size image.Point, splitTop, splitBottom float64, featherSize int) *image.Alpha {
	width, height := size.X, size.Y
	topX := int(float64(width) * splitTop)
	bottomX := int(float64(width) * splitBottom)
	points := []pointF{
		{X: float64(topX - 5), Y: 0},
		{X: float64(topX - 5 + featherSize/3), Y: 0},
		{X: float64(bottomX - 5 + featherSize/3), Y: float64(height)},
		{X: float64(bottomX - 5), Y: float64(height)},
	}
	mask := polygonMask(width, height, points)
	return mask
}

func closeFace(face font.Face) {
	type closer interface{ Close() error }
	if c, ok := face.(closer); ok {
		_ = c.Close()
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func scaleUint8(v uint8, factor float64) uint8 {
	x := int(math.Round(float64(v) * factor))
	if x < 0 {
		return 0
	}
	if x > 255 {
		return 255
	}
	return uint8(x)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// rotateBicubic rotates src by angle (degrees, counter-clockwise) and returns
// a new NRGBA image sized to the rotated content's bounding box (expand=true).
//
// It uses golang.org/x/image/draw.CatmullRom (true bicubic resampling) instead
// of disintegration/imaging.Rotate's bilinear kernel. The bilinear kernel was
// the root cause of the "split/dark edges on the upper-right of right-side
// posters": when a poster's anti-aliased rounded corner or shadow edge sat on
// the column image boundary, the bilinear 2x2 tap crossed into the transparent
// padding and blended premultiplied alpha down to a dark fringe. CatmullRom's
// 4x4 support also samples transparent neighbors, but its negative lobes
// reconstruct sharp alpha transitions instead of averaging them into gray.
//
// The transform maps src pixel (sx,sy) -> dst, with the source centered so the
// rotation is about its own center, matching PIL's Image.rotate(expand=True).
func rotateBicubic(src image.Image, angle float64) *image.NRGBA {
	angle = angle - math.Floor(angle/360)*360
	if angle == 0 {
		return toNRGBA(src)
	}

	base := toNRGBA(src)
	srcB := base.Bounds()
	srcW := srcB.Dx()
	srcH := srcB.Dy()
	if srcW <= 0 || srcH <= 0 {
		return image.NewNRGBA(image.Rect(0, 0, 0, 0))
	}

	// Special-case 90/180/270 to avoid the affine setup entirely; these are
	// lossless and have no edge artifacts.
	switch {
	case angle == 90:
		return imaging.Rotate90(base)
	case angle == 180:
		return imaging.Rotate180(base)
	case angle == 270:
		return imaging.Rotate270(base)
	}

	rad := math.Pi * angle / 180.0
	sin, cos := math.Sincos(rad)

	// Forward map the source rectangle's four corners to find the destination
	// bounding box (expand=true). Rotation is about the source center.
	// A source point (sx,sy) relative to center (cw,ch) maps to:
	//   dx = cos*(sx-cw) - sin*(sy-ch)
	//   dy = sin*(sx-cw) + cos*(sy-ch)
	cw := float64(srcW) / 2.0
	ch := float64(srcH) / 2.0
	corners := [][2]float64{
		{0, 0},
		{float64(srcW), 0},
		{float64(srcW), float64(srcH)},
		{0, float64(srcH)},
	}
	minX, minY := math.Inf(+1), math.Inf(+1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, c := range corners {
		ox := c[0] - cw
		oy := c[1] - ch
		dx := cos*ox - sin*oy + cw
		dy := sin*ox + cos*oy + ch
		if dx < minX {
			minX = dx
		}
		if dx > maxX {
			maxX = dx
		}
		if dy < minY {
			minY = dy
		}
		if dy > maxY {
			maxY = dy
		}
	}
	dstW := int(math.Ceil(maxX) - math.Floor(minX))
	dstH := int(math.Ceil(maxY) - math.Floor(minY))
	if dstW <= 0 || dstH <= 0 {
		return image.NewNRGBA(image.Rect(0, 0, 0, 0))
	}

	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))

	// Build the s2d (src->dst) affine, then shift so the bounding box origin
	// lands at dst (0,0). Aff3 layout (row-major, [a b c; d e f]):
	//   dx = a*sx + b*sy + c
	//   dy = d*sx + e*sy + f
	// Forward rotation about center: translate to origin, rotate, translate
	// back, then shift by -min to align bbox to (0,0).
	s2d := f64.Aff3{
		cos, -sin, cw - cos*cw + sin*ch - minX,
		sin, cos, ch - sin*cw - cos*ch - minY,
	}

	// sr is the full source bounds. CatmullRom.Transform with op=Over will
	// alpha-composite the rotated source onto the (zero) destination, leaving
	// fully transparent pixels as transparent black — exactly the expand=true
	// behavior we want, without the dark-fringe artifacts.
	xdraw.CatmullRom.Transform(
		dst, s2d,
		base, srcB,
		xdraw.Over, nil,
	)
	return dst
}
