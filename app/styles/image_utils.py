from PIL import Image


def blend_with_color(image, color, ratio):
    base = image if image.mode == "RGB" else image.convert("RGB")
    alpha = max(0.0, min(1.0, float(ratio)))
    overlay = Image.new("RGB", base.size, tuple(int(channel) for channel in color[:3]))
    return Image.blend(base, overlay, alpha)


def add_film_grain(image, intensity=0.05):
    strength = max(0.0, float(intensity))
    if strength <= 0:
        return image if image.mode == "RGB" else image.convert("RGB")

    base = image if image.mode == "RGB" else image.convert("RGB")
    sigma = max(4.0, min(64.0, strength * 180.0))
    noise = Image.effect_noise(base.size, sigma).convert("L")
    noise_rgb = Image.merge("RGB", (noise, noise, noise))
    return Image.blend(base, noise_rgb, min(0.18, strength * 0.9))


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
