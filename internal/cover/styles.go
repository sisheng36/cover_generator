package cover

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/disintegration/imaging"

	"embytool/internal/fonts"
)

var styleCanvasSize = image.Pt(1920, 1080)

func measureTextForFont(fontCache *fonts.Cache, path string, size float64, text string) (textMetrics, error) {
	face, err := fontCache.LoadFace(path, size)
	if err != nil {
		return textMetrics{}, err
	}
	defer closeFace(face)
	return measureText(face, text), nil
}

func drawSingleStyleTitleText(canvas *image.NRGBA, title [2]string, fontPaths [2]string, fontCache *fonts.Cache, fontSize [2]float64, bgColor color.NRGBA) (*image.NRGBA, error) {
	titleZh, titleEn := title[0], title[1]
	zhFontPath, enFontPath := fontPaths[0], fontPaths[1]
	zhFontSizeRatio, enFontSizeRatio := fontSize[0], fontSize[1]

	leftAreaCenterX := int(float64(styleCanvasSize.X) * 0.25)
	leftAreaCenterY := styleCanvasSize.Y / 2
	zhFontSize := int(float64(styleCanvasSize.Y) * 0.17 * zhFontSizeRatio)
	enFontSize := int(float64(styleCanvasSize.Y) * 0.07 * enFontSizeRatio)

	zhFace, err := fontCache.LoadFace(zhFontPath, float64(zhFontSize))
	if err != nil {
		return nil, err
	}
	defer closeFace(zhFace)

	enFace, err := fontCache.LoadFace(enFontPath, float64(enFontSize))
	if err != nil {
		return nil, err
	}
	defer closeFace(enFace)

	zhMetrics := measureText(zhFace, titleZh)
	zhX := float64(leftAreaCenterX - zhMetrics.Width/2)
	zhY := float64(leftAreaCenterY-zhMetrics.Height-enFontSize/2-5)

	textLayer := image.NewNRGBA(canvas.Bounds())
	shadowLayer := image.NewNRGBA(canvas.Bounds())
	textColor := color.NRGBA{R: 255, G: 255, B: 255, A: 229}
	textShadowColor := darkenColor(bgColor, 0.8)
	textShadowColor.A = 75
	shadowOffset := 12

	for offset := 3; offset <= shadowOffset; offset += 2 {
		drawString(shadowLayer, titleZh, zhX+float64(offset), zhY+float64(offset), zhFace, textShadowColor)
	}
	drawString(textLayer, titleZh, zhX, zhY, zhFace, textColor)

	if strings.TrimSpace(titleEn) != "" {
		enMetrics := measureText(enFace, titleEn)
		enX := float64(leftAreaCenterX - enMetrics.Width/2)
		enY := zhY + float64(zhMetrics.Height) + float64(enFontSize)
		for offset := 2; offset <= shadowOffset/2; offset++ {
			drawString(shadowLayer, titleEn, enX+float64(offset), enY+float64(offset), enFace, textShadowColor)
		}
		drawString(textLayer, titleEn, enX, enY, enFace, textColor)
	}

	blurredShadow := imaging.Blur(shadowLayer, float64(shadowOffset))
	draw.Draw(canvas, canvas.Bounds(), blurredShadow, image.Point{}, draw.Over)
	draw.Draw(canvas, canvas.Bounds(), textLayer, image.Point{}, draw.Over)
	return canvas, nil
}

func createStyleSingle1(imagePath string, title [2]string, fontPaths [2]string, fontCache *fonts.Cache, fontSize [2]float64, blurSize int, colorRatio float64, itemCount int, badge badgeConfig) ([]byte, error) {
	zhFontPath := fontPaths[0]
	zhFontSizeRatio, enFontSizeRatio := fontSize[0], fontSize[1]

	if blurSize < 0 {
		blurSize = 50
	}
	if colorRatio < 0 || colorRatio > 1 {
		colorRatio = 0.8
	}
	if zhFontSizeRatio <= 0 {
		zhFontSizeRatio = 1
	}
	if enFontSizeRatio <= 0 {
		enFontSizeRatio = 1
	}

	src, err := loadImageFile(imagePath)
	if err != nil {
		return nil, err
	}
	originalImg := toNRGBA(src)

	candidateColors := findDominantMacaronColors(originalImg, 6)
	if len(candidateColors) > 1 {
		rand.Shuffle(len(candidateColors), func(i, j int) {
			candidateColors[i], candidateColors[j] = candidateColors[j], candidateColors[i]
		})
	}
	extractedColors := append([]color.NRGBA{}, candidateColors...)
	softMacaronColors := []color.NRGBA{
		{R: 237, G: 159, B: 77, A: 255},
		{R: 186, G: 225, B: 255, A: 255},
		{R: 255, G: 223, B: 186, A: 255},
		{R: 202, G: 231, B: 200, A: 255},
	}
	for len(extractedColors) < 6 {
		if len(extractedColors) == 0 {
			extractedColors = append(extractedColors, softMacaronColors[rand.Intn(len(softMacaronColors))])
			continue
		}
		bestColor := softMacaronColors[0]
		bestScore := -1.0
		for _, candidate := range softMacaronColors {
			minDist := math.MaxFloat64
			for _, existing := range extractedColors {
				if dist := colorDistance(candidate, existing); dist < minDist {
					minDist = dist
				}
			}
			if minDist > bestScore {
				bestScore = minDist
				bestColor = candidate
			}
		}
		extractedColors = append(extractedColors, bestColor)
	}

	bgColor := darkenColor(extractedColors[0], 0.85)
	baseColorForBadge := extractedColors[0]
	cardColors := []color.NRGBA{extractedColors[1], extractedColors[2]}

	bgImg := imaging.Fill(originalImg, styleCanvasSize.X, styleCanvasSize.Y, imaging.Center, imaging.Lanczos)
	bgImg = imaging.Blur(bgImg, float64(blurSize))
	blendedBgImg := addFilmGrain(blendWithColor(bgImg, bgColor, colorRatio), 0.03)

	canvas := image.NewNRGBA(image.Rect(0, 0, styleCanvasSize.X, styleCanvasSize.Y))
	draw.Draw(canvas, canvas.Bounds(), blendedBgImg, image.Point{}, draw.Over)

	squareImg := cropToSquare(originalImg)
	cardSize := int(float64(styleCanvasSize.Y) * 0.7)
	squareImg = imaging.Resize(squareImg, cardSize, cardSize, imaging.Lanczos)

	mainCard := addRoundedCornersHighRes(squareImg, cardSize/8)

	auxCard1Bg := imaging.Blur(squareImg, 8)
	auxCard1 := addRoundedCornersHighRes(blendWithColor(auxCard1Bg, cardColors[0], 0.5), cardSize/8)

	auxCard2Bg := imaging.Blur(squareImg, 16)
	auxCard2 := addRoundedCornersHighRes(blendWithColor(auxCard2Bg, cardColors[1], 0.6), cardSize/8)

	centerPos := image.Pt(styleCanvasSize.X-int(float64(styleCanvasSize.Y)*0.5), styleCanvasSize.Y/2)
	rotationAngles := []float64{36, 18, 0}
	shadowConfigs := []struct {
		offset image.Point
		radius int
		opacity float64
	}{
		{offset: image.Pt(10, 16), radius: 12, opacity: 0.4},
		{offset: image.Pt(15, 22), radius: 15, opacity: 0.5},
		{offset: image.Pt(20, 26), radius: 18, opacity: 0.6},
	}
	cardsCanvas := image.NewNRGBA(canvas.Bounds())
	for idx, card := range []*image.NRGBA{auxCard2, auxCard1, mainCard} {
		cardsCanvas = addShadowAndRotate(cardsCanvas, card, rotationAngles[idx], shadowConfigs[idx].offset, shadowConfigs[idx].radius, shadowConfigs[idx].opacity, centerPos)
	}
	canvas = imaging.Overlay(canvas, cardsCanvas, image.Point{}, 1)

	canvas, err = drawSingleStyleTitleText(canvas, title, fontPaths, fontCache, [2]float64{zhFontSizeRatio, enFontSizeRatio}, bgColor)
	if err != nil {
		return nil, err
	}

	if badge.Show && itemCount > 0 {
		canvas, err = drawBadge(canvas, itemCount, fontCache, zhFontPath, badge.Style, badge.SizeRatio, baseColorForBadge)
		if err != nil {
			return nil, err
		}
	}

	return imageToPNGBytes(canvas)
}

func createStyleSingle2(imagePath string, title [2]string, fontPaths [2]string, fontCache *fonts.Cache, fontSize [2]float64, blurSize int, colorRatio float64, itemCount int, badge badgeConfig) ([]byte, error) {
	zhFontPath := fontPaths[0]
	zhFontSizeRatio, enFontSizeRatio := fontSize[0], fontSize[1]

	if blurSize < 0 {
		blurSize = 50
	}
	if colorRatio < 0 || colorRatio > 1 {
		colorRatio = 0.8
	}
	if zhFontSizeRatio <= 0 {
		zhFontSizeRatio = 1
	}
	if enFontSizeRatio <= 0 {
		enFontSizeRatio = 1
	}

	src, err := loadImageFile(imagePath)
	if err != nil {
		return nil, err
	}
	originalImg := toNRGBA(src)
	fgImg := alignImageRight(originalImg, styleCanvasSize)

	vibrantColors := findDominantVibrantColors(fgImg, 5)
	softColors := []color.NRGBA{
		{R: 237, G: 159, B: 77, A: 255},
		{R: 255, G: 183, B: 197, A: 255},
		{R: 186, G: 225, B: 255, A: 255},
		{R: 255, G: 223, B: 186, A: 255},
		{R: 202, G: 231, B: 200, A: 255},
		{R: 245, G: 203, B: 255, A: 255},
	}
	bgColor := softColors[rand.Intn(len(softColors))]
	if len(vibrantColors) > 0 {
		bgColor = vibrantColors[0]
	}
	baseColorForBadge := bgColor
	shadowColor := darkenColor(bgColor, 0.5)

	bgImg := imaging.Fill(originalImg, styleCanvasSize.X, styleCanvasSize.Y, imaging.Center, imaging.Lanczos)
	bgImg = imaging.Blur(bgImg, float64(blurSize))
	bgColor = darkenColor(bgColor, 0.85)
	blendedBgImg := addFilmGrain(blendWithColor(bgImg, bgColor, colorRatio), 0.05)

	diagonalMask := createDiagonalMask(styleCanvasSize, 0.55, 0.4)
	shadowMask := createShadowMask(styleCanvasSize, 0.55, 0.4, 30)
	canvas := image.NewNRGBA(image.Rect(0, 0, styleCanvasSize.X, styleCanvasSize.Y))
	draw.Draw(canvas, canvas.Bounds(), fgImg, image.Point{}, draw.Over)
	tempCanvas := imaging.Clone(canvas)
	shadowLayer := imaging.New(styleCanvasSize.X, styleCanvasSize.Y, color.NRGBA{R: shadowColor.R, G: shadowColor.G, B: shadowColor.B, A: 255})
	draw.DrawMask(tempCanvas, tempCanvas.Bounds(), shadowLayer, image.Point{}, shadowMask, image.Point{}, draw.Over)
	finalCanvas := imaging.Clone(tempCanvas)
	draw.DrawMask(finalCanvas, finalCanvas.Bounds(), blendedBgImg, image.Point{}, diagonalMask, image.Point{}, draw.Over)

	finalCanvas, err = drawSingleStyleTitleText(finalCanvas, title, fontPaths, fontCache, [2]float64{zhFontSizeRatio, enFontSizeRatio}, bgColor)
	if err != nil {
		return nil, err
	}

	if badge.Show && itemCount > 0 {
		finalCanvas, err = drawBadge(finalCanvas, itemCount, fontCache, zhFontPath, badge.Style, badge.SizeRatio, baseColorForBadge)
		if err != nil {
			return nil, err
		}
	}

	return imageToPNGBytes(finalCanvas)
}

func createStyleMulti1(libraryDir string, title [2]string, fontPaths [2]string, fontCache *fonts.Cache, fontSize [2]float64, isBlur bool, blurSize int, colorRatio float64, itemCount int, badge badgeConfig) ([]byte, error) {
	zhFontPath, enFontPath := fontPaths[0], fontPaths[1]
	titleZh, titleEn := title[0], title[1]
	zhFontSizeRatio, enFontSizeRatio := fontSize[0], fontSize[1]
	if blurSize < 0 {
		blurSize = 50
	}
	if colorRatio < 0 || colorRatio > 1 {
		colorRatio = 0.8
	}
	if zhFontSizeRatio <= 0 {
		zhFontSizeRatio = 1
	}
	if enFontSizeRatio <= 0 {
		enFontSizeRatio = 1
	}

	firstImagePath := filepath.Join(libraryDir, "1.jpg")
	firstImage, err := loadImageFile(firstImagePath)
	if err != nil {
		return nil, err
	}
	colorImg := toNRGBA(firstImage)
	vibrantColors := findDominantVibrantColors(colorImg, 5)
	softColors := []color.NRGBA{
		{R: 237, G: 159, B: 77, A: 255},
		{R: 255, G: 183, B: 197, A: 255},
		{R: 186, G: 225, B: 255, A: 255},
		{R: 255, G: 223, B: 186, A: 255},
		{R: 202, G: 231, B: 200, A: 255},
		{R: 245, G: 203, B: 255, A: 255},
	}
	blurColor := softColors[rand.Intn(len(softColors))]
	if len(vibrantColors) > 0 {
		blurColor = vibrantColors[0]
	}
	baseColorForBadge := blurColor
	gradientColors := getPosterPrimaryColors(firstImagePath)

	var coloredBgImg *image.NRGBA
	if isBlur {
		coloredBgImg, err = createBlurBackground(firstImagePath, styleCanvasSize.X, styleCanvasSize.Y, blurColor, blurSize, colorRatio, 0.6)
		if err != nil {
			return nil, err
		}
	} else {
		coloredBgImg = createGradientBackground(styleCanvasSize.X, styleCanvasSize.Y, gradientColors)
	}

	supportedFormats := map[string]struct{}{
		".jpg":  {},
		".jpeg": {},
		".png":  {},
		".bmp":  {},
		".gif":  {},
		".webp": {},
	}
	customOrder := "315426987"
	orderMap := map[string]int{}
	for i, ch := range customOrder {
		orderMap[string(ch)] = i
	}
	entries, err := os.ReadDir(libraryDir)
	if err != nil {
		return nil, err
	}
	type posterFile struct {
		path  string
		order int
	}
	var posterFiles []posterFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		base := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		order, ok := orderMap[base]
		if !ok {
			continue
		}
		if _, ok := supportedFormats[ext]; !ok {
			continue
		}
		posterFiles = append(posterFiles, posterFile{path: filepath.Join(libraryDir, entry.Name()), order: order})
	}
	sort.Slice(posterFiles, func(i, j int) bool { return posterFiles[i].order < posterFiles[j].order })
	if len(posterFiles) == 0 {
		return nil, fmt.Errorf("no poster files found")
	}
	if len(posterFiles) > 9 {
		posterFiles = posterFiles[:9]
	}
	paths := make([]string, 0, len(posterFiles))
	for _, p := range posterFiles {
		paths = append(paths, p.path)
	}

	rows := 3
	cols := 3
	margin := 22
	cornerRadius := 46
	rotationAngle := -15.8
	startX := 1050
	startY := -362
	columnSpacing := 100
	cellWidth := 410
	cellHeight := 610
	shadowExtra := 40

	result := imaging.Clone(coloredBgImg)
	posterGroup := image.NewNRGBA(result.Bounds())

	groupedPosters := make([][]string, 0)
	for i := 0; i < len(paths); i += rows {
		end := i + rows
		if end > len(paths) {
			end = len(paths)
		}
		groupedPosters = append(groupedPosters, paths[i:end])
	}

	for colIndex, columnPosters := range groupedPosters {
		if colIndex >= cols {
			break
		}
		columnX := startX + colIndex*columnSpacing
		columnHeight := rows*cellHeight + (rows-1)*margin
		columnImage := image.NewNRGBA(image.Rect(0, 0, cellWidth+shadowExtra, columnHeight+shadowExtra))
		for rowIndex, posterPath := range columnPosters {
			posterSource, err := loadImageFile(posterPath)
			if err != nil {
				continue
			}
			poster := imaging.Fill(posterSource, cellWidth, cellHeight, imaging.Center, imaging.Lanczos)
			if cornerRadius > 0 {
				poster = addRoundedCorners(poster, cornerRadius)
			}
			posterWithShadow := addShadow(poster, image.Pt(17, 14), color.NRGBA{0, 0, 0, 188}, 16)
			yPosition := rowIndex * (cellHeight + margin)
			draw.Draw(columnImage, image.Rect(0, yPosition, posterWithShadow.Bounds().Dx(), yPosition+posterWithShadow.Bounds().Dy()), posterWithShadow, image.Point{}, draw.Over)
		}

		// 与 Python 参考一致：先把整列图居中放进更大的透明旋转画布再旋转，
		// 使列内容（含阴影、圆角抗锯齿边）远离旋转边界，避免贴紧边界旋转时
		// 双线性插值对透明背景取样产生的暗色毛刺，以及右列因此看上去偏右被裁切。
		colW := columnImage.Bounds().Dx()
		colH := columnImage.Bounds().Dy()
		rotationCanvasSize := int(math.Sqrt(float64(colW*colW+colH*colH)) * 1.5)
		rotationCanvas := image.NewNRGBA(image.Rect(0, 0, rotationCanvasSize, rotationCanvasSize))
		pasteX := (rotationCanvasSize - colW) / 2
		pasteY := (rotationCanvasSize - colH) / 2
		draw.Draw(rotationCanvas, image.Rect(pasteX, pasteY, pasteX+colW, pasteY+colH), columnImage, image.Point{}, draw.Over)
		rotatedColumn := rotateBicubic(rotationCanvas, rotationAngle)
		columnCenterY := startY + columnHeight/2
		columnCenterX := columnX
		if colIndex == 1 {
			columnCenterX += cellWidth - 50
		} else if colIndex == 2 {
			columnCenterY += -155
			columnCenterX += cellWidth*2 - 40
		}
		finalX := columnCenterX - rotatedColumn.Bounds().Dx()/2
		finalY := columnCenterY - rotatedColumn.Bounds().Dy()/2
		draw.Draw(posterGroup, image.Rect(finalX, finalY, finalX+rotatedColumn.Bounds().Dx(), finalY+rotatedColumn.Bounds().Dy()), rotatedColumn, image.Point{}, draw.Over)
	}

	posterGroupShadow := createShadowLayer(posterGroup, image.Pt(26, 22), color.NRGBA{0, 0, 0, 76}, 28)
	result = imaging.Overlay(result, posterGroupShadow, image.Point{}, 1)
	result = imaging.Overlay(result, posterGroup, image.Point{}, 1)

	randomColor := getRandomColor(paths[0])
	textShadowColor := darkenColor(blurColor, 0.8)
	textShadowColor.A = 75
	zhFontSize := int(float64(styleCanvasSize.Y) * 0.17 * zhFontSizeRatio)
	result, err = drawTextOnImage(result, titleZh, pointF{X: 73.32, Y: 427.34}, fontCache, zhFontPath, zhFontSize, color.NRGBA{R: 255, G: 255, B: 255, A: 229}, isBlur, textShadowColor, 12, 75)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(titleEn) != "" {
		baseFontSize := 50.0 * enFontSizeRatio
		lineSpacing := baseFontSize * 0.1
		words := strings.Fields(titleEn)
		wordCount := len(words)
		maxCharsPerLine := 0
		for _, word := range words {
			if len(word) > maxCharsPerLine {
				maxCharsPerLine = len(word)
			}
		}
		fontSizeFloat := baseFontSize
		if maxCharsPerLine > 10 || wordCount > 3 {
			denom := math.Max(float64(maxCharsPerLine), float64(wordCount*3))
			fontSizeFloat = baseFontSize * math.Pow(10/denom, 0.8)
		}
		if fontSizeFloat < 30 {
			fontSizeFloat = 30
		}
		lineCount := 1
		result, lineCount, err = drawMultilineTextOnImage(result, titleEn, pointF{X: 124.68, Y: 624.55}, fontCache, enFontPath, int(fontSizeFloat), lineSpacing, color.NRGBA{R: 255, G: 255, B: 255, A: 229}, isBlur, textShadowColor, 4, 100)
		if err != nil {
			return nil, err
		}
		colorBlockHeight := baseFontSize + lineSpacing + float64(lineCount-1)*(float64(int(fontSizeFloat))+lineSpacing)
		result = drawColorBlock(result, pointF{X: 84.38, Y: 620.06}, pointF{X: 21.51, Y: colorBlockHeight}, randomColor)
	}

	if badge.Show && itemCount > 0 {
		result, err = drawBadge(result, itemCount, fontCache, zhFontPath, badge.Style, badge.SizeRatio, baseColorForBadge)
		if err != nil {
			return nil, err
		}
	}

	return imageToPNGBytes(result)
}

type badgeConfig struct {
	Show      bool
	Style     string
	SizeRatio float64
}
