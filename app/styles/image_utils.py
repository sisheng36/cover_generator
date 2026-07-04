from PIL import Image, ImageFont

# 复用同一 (path, size) 的字体对象，避免每次封面生成都重新读盘与构造。
_font_cache: dict = {}


def load_font(path, size):
    key = (str(path), int(size))
    font = _font_cache.get(key)
    if font is None:
        font = ImageFont.truetype(str(path), int(size))
        _font_cache[key] = font
    return font


def blend_with_color(image, color, ratio):
    base = image if image.mode == "RGB" else image.convert("RGB")
    alpha = max(0.0, min(1.0, float(ratio)))
    overlay = Image.new("RGB", base.size, tuple(int(channel) for channel in color[:3]))
    try:
        return Image.blend(base, overlay, alpha)
    finally:
        overlay.close()


def add_film_grain(image, intensity=0.05):
    strength = max(0.0, float(intensity))
    if strength <= 0:
        return image if image.mode == "RGB" else image.convert("RGB")

    base = image if image.mode == "RGB" else image.convert("RGB")
    sigma = max(4.0, min(64.0, strength * 180.0))
    noise = Image.effect_noise(base.size, sigma).convert("L")
    try:
        noise_rgb = Image.merge("RGB", (noise, noise, noise))
        try:
            return Image.blend(base, noise_rgb, min(0.18, strength * 0.9))
        finally:
            noise_rgb.close()
    finally:
        noise.close()


def create_horizontal_gradient_mask(size, power=0.7):
    width, height = size
    if width <= 0 or height <= 0:
        raise ValueError(f"invalid mask size: {size!r}")

    if width == 1:
        row = bytes((255,))
    else:
        row = bytes(
            int(255 * ((index / (width - 1)) ** power))
            for index in range(width)
        )

    return Image.frombytes("L", (width, 1), row).resize(
        (width, height),
        Image.Resampling.NEAREST,
    )
