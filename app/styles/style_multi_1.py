import logging
import os
import random
import math
import io
import colorsys
from pathlib import Path
from collections import Counter
from PIL import Image, ImageDraw, ImageFilter, ImageOps

from .badge_drawer import draw_badge
from .image_utils import add_film_grain, blend_with_color, create_horizontal_gradient_mask, load_font

logger = logging.getLogger(__name__)

POSTER_GEN_CONFIG = {
    "ROWS": 3,
    "COLS": 3,
    "MARGIN": 22,
    "CORNER_RADIUS": 46.1,
    "ROTATION_ANGLE": -15.8,
    "START_X": 1050,
    "START_Y": -362,
    "COLUMN_SPACING": 100,
    "SAVE_COLUMNS": True,
    "CELL_WIDTH": 410,
    "CELL_HEIGHT": 610,
    "CANVAS_WIDTH": 1920,
    "CANVAS_HEIGHT": 1080,
}

def is_not_black_white_gray_near(color, threshold=20):
    r, g, b = color
    if (r < threshold and g < threshold and b < threshold) or \
       (r > 255 - threshold and g > 255 - threshold and b > 255 - threshold):
        return False
    gray_diff_threshold = 10
    if abs(r - g) < gray_diff_threshold and abs(g - b) < gray_diff_threshold and abs(r - b) < gray_diff_threshold:
        return False
    return True

def rgb_to_hsv(color):
    r, g, b = [x / 255.0 for x in color]
    return colorsys.rgb_to_hsv(r, g, b)

def hsv_to_rgb(h, s, v):
    r, g, b = colorsys.hsv_to_rgb(h, s, v)
    return (int(r * 255), int(g * 255), int(b * 255))

def adjust_to_macaron(h, s, v, target_saturation_range=(0.2, 0.7), target_value_range=(0.55, 0.85)):
    adjusted_s = min(max(s, target_saturation_range[0]), target_saturation_range[1])
    adjusted_v = min(max(v, target_value_range[0]), target_value_range[1])
    return adjusted_s, adjusted_v

def find_dominant_vibrant_colors(image, num_colors=5):
    img = image.copy()
    img.thumbnail((100, 100))
    img = img.convert('RGB')
    pixels = list(img.getdata())
    filtered_pixels = [p for p in pixels if is_not_black_white_gray_near(p)]
    if not filtered_pixels: return []
    color_counter = Counter(filtered_pixels)
    dominant_colors = color_counter.most_common(num_colors * 3)
    macaron_colors = []
    seen_hues = set()
    for color, count in dominant_colors:
        h, s, v = rgb_to_hsv(color)
        adjusted_s, adjusted_v = adjust_to_macaron(h, s, v)
        adjusted_rgb = hsv_to_rgb(h, adjusted_s, adjusted_v)
        hue_degree = int(h * 360)
        is_similar_hue = any(abs(hue_degree - seen) < 15 for seen in seen_hues)
        if not is_similar_hue and adjusted_rgb not in macaron_colors:
            macaron_colors.append(adjusted_rgb)
            seen_hues.add(hue_degree)
            if len(macaron_colors) >= num_colors: break
    return macaron_colors

def darken_color(color, factor=0.7):
    r, g, b = color
    return (int(r * factor), int(g * factor), int(b * factor))

def add_shadow(img, offset=(5, 5), shadow_color=(0, 0, 0, 100), blur_radius=3):
    base = img if img.mode == "RGBA" else img.convert("RGBA")
    shadow_mask = base.getchannel("A")

    # 双层阴影：先给一个贴边的“接触阴影”，再叠一层更柔和的投影，
    # 右侧与底部会更有浮起感，适合多图海报墙。
    shadow_layers = [
        (
            max(1, int(offset[0] * 0.45)),
            max(1, int(offset[1] * 0.45)),
            max(2, int(blur_radius * 0.42)),
            max(0, min(255, int(shadow_color[3] * 0.62))),
        ),
        (
            int(offset[0]),
            int(offset[1]),
            int(blur_radius),
            max(0, min(255, int(shadow_color[3]))),
        ),
    ]
    max_blur = max(layer[2] for layer in shadow_layers)
    max_offset_x = max(layer[0] for layer in shadow_layers)
    max_offset_y = max(layer[1] for layer in shadow_layers)
    shadow_width = base.width + max_offset_x + max_blur * 2
    shadow_height = base.height + max_offset_y + max_blur * 2
    shadow = Image.new("RGBA", (shadow_width, shadow_height), (0, 0, 0, 0))

    try:
        for off_x, off_y, layer_blur, layer_alpha in shadow_layers:
            layer = Image.new("RGBA", base.size, shadow_color[:3] + (layer_alpha,))
            layer_canvas = Image.new("RGBA", shadow.size, (0, 0, 0, 0))
            layer_canvas.paste(layer, (max_blur + off_x, max_blur + off_y), shadow_mask)
            layer.close()
            blurred = layer_canvas.filter(ImageFilter.GaussianBlur(layer_blur))
            layer_canvas.close()
            merged = Image.alpha_composite(shadow, blurred)
            shadow.close()
            blurred.close()
            shadow = merged

        result = Image.new("RGBA", shadow.size, (0, 0, 0, 0))
        result.paste(base, (max_blur, max_blur), base)
        out = Image.alpha_composite(shadow, result)
        result.close()
        return out
    finally:
        shadow_mask.close()
        shadow.close()
        if base is not img:
            base.close()

def create_shadow_layer(img, offset=(5, 5), shadow_color=(0, 0, 0, 100), blur_radius=3):
    base = img if img.mode == "RGBA" else img.convert("RGBA")
    shadow = Image.new("RGBA", base.size, (0, 0, 0, 0))
    shadow_source = Image.new("RGBA", base.size, shadow_color)
    shadow_mask = base.getchannel("A")
    try:
        shadow.paste(shadow_source, offset, shadow_mask)
        return shadow.filter(ImageFilter.GaussianBlur(blur_radius))
    finally:
        shadow_source.close()
        shadow_mask.close()
        if base is not img:
            base.close()

def draw_text_on_image(image, text, position, font_path, default_font_path, font_size, fill_color=(255, 255, 255, 255), shadow=False, shadow_color=None, shadow_offset=10, shadow_alpha=75):
    base = image if image.mode == "RGBA" else image.convert("RGBA")
    text_layer = Image.new('RGBA', base.size, (255, 255, 255, 0))
    shadow_layer = Image.new('RGBA', base.size, (0, 0, 0, 0))
    draw = ImageDraw.Draw(text_layer)
    shadow_draw = ImageDraw.Draw(shadow_layer)
    font = load_font(font_path, font_size)
    if shadow:
        fill_color = (fill_color[0], fill_color[1], fill_color[2], 229)
        if shadow_color is None:
            r, g, b = [max(0, int(c * 0.7)) for c in fill_color[:3]]
            shadow_color_with_alpha = (r, g, b, shadow_alpha)
        else:
            shadow_color_with_alpha = shadow_color[:3] + (shadow_alpha,)
        for offset in range(3, shadow_offset + 1, 2):
            shadow_draw.text((position[0] + offset, position[1] + offset), text, font=font, fill=shadow_color_with_alpha)
    draw.text(position, text, font=font, fill=fill_color)
    blurred_shadow = shadow_layer.filter(ImageFilter.GaussianBlur(radius=shadow_offset))
    shadow_layer.close()
    combined = Image.alpha_composite(base, blurred_shadow)
    blurred_shadow.close()
    if base is not image:
        base.close()
    result = Image.alpha_composite(combined, text_layer)
    combined.close()
    text_layer.close()
    return result

def draw_multiline_text_on_image(image, text, position, font_path, default_font_path, font_size, line_spacing=10, fill_color=(255, 255, 255, 255), shadow=False, shadow_color=None, shadow_offset=4, shadow_alpha=100):
    base = image if image.mode == "RGBA" else image.convert("RGBA")
    text_layer = Image.new('RGBA', base.size, (255, 255, 255, 0))
    draw = ImageDraw.Draw(text_layer)
    font = load_font(font_path, font_size)
    lines = text.split(" ")
    if shadow:
        fill_color = (fill_color[0], fill_color[1], fill_color[2], 229)
        if shadow_color is None:
            r, g, b = [max(0, int(c * 0.7)) for c in fill_color[:3]]
            shadow_color_with_alpha = (r, g, b, shadow_alpha)
        else:
            shadow_color_with_alpha = shadow_color[:3] + (shadow_alpha,)
    if len(lines) <= 1:
        if shadow:
            for offset in range(3, shadow_offset + 1, 2):
                draw.text((position[0] + offset, position[1] + offset), text, font=font, fill=shadow_color_with_alpha)
        draw.text(position, text, font=font, fill=fill_color)
        result = Image.alpha_composite(base, text_layer)
        if base is not image:
            base.close()
        text_layer.close()
        return result, 1
    x, y = position
    for i, line in enumerate(lines):
        current_y = y + i * (font_size + line_spacing)
        if shadow:
            for offset in range(3, shadow_offset + 1, 2):
                draw.text((x + offset, current_y + offset), line, font=font, fill=shadow_color_with_alpha)
        draw.text((x, current_y), line, font=font, fill=fill_color)
    result = Image.alpha_composite(base, text_layer)
    if base is not image:
        base.close()
    text_layer.close()
    return result, len(lines)

def get_random_color(image_path):
    try:
        with Image.open(image_path) as img:
            width, height = img.size
            random_x = random.randint(int(width * 0.5), int(width * 0.8))
            random_y = random.randint(int(height * 0.5), int(height * 0.8))
            pixel = img.getpixel((random_x, random_y))
        return pixel[:3] + (255,) if isinstance(pixel, tuple) else (pixel, pixel, pixel, 255)
    except Exception:
        return (random.randint(50, 200), random.randint(50, 200), random.randint(50, 200), 255)

def draw_color_block(image, position, size, color):
    draw = ImageDraw.Draw(image)
    draw.rectangle([position, (position[0] + size[0], position[1] + size[1])], fill=color)
    return image

def create_gradient_background(width, height, color=None):
    def _normalize_rgb(input_rgb):
        if isinstance(input_rgb, tuple):
            if len(input_rgb) == 2 and isinstance(input_rgb[0], tuple): return _normalize_rgb(input_rgb[0])
            if len(input_rgb) >= 3: return input_rgb[:3]
        raise ValueError(f"无法识别的颜色格式: {input_rgb!r}")
    def _is_mid_bright_hsl(input_rgb, min_l=0.3, max_l=0.7):
        r, g, b = [c/255.0 for c in _normalize_rgb(input_rgb)]
        h, l, s = colorsys.rgb_to_hls(r, g, b)
        return min_l <= l <= max_l
    selected_color = None
    if isinstance(color, list) and color:
        for c in color:
            if _is_mid_bright_hsl(c):
                selected_color = _normalize_rgb(c)
                break
    if selected_color is None:
        h = random.uniform(0, 1)
        l = random.uniform(0.5, 0.8)
        s = random.uniform(0.5, 1.0)
        r, g, b = [int(c*255) for c in colorsys.hls_to_rgb(h, l, s)]
        selected_color = (r, g, b)
    r, g, b = [int(c * 0.65) for c in selected_color]
    color1 = (max(0, r), max(0, g), max(0, b), 255)
    r2, g2, b2 = [min(255, int(c * 1.9)) for c in color1[:3]]
    color2 = (r2, g2, b2, 255)
    left_image = Image.new("RGBA", (width, height), color1)
    right_image = Image.new("RGBA", (width, height), color2)
    mask = create_horizontal_gradient_mask((width, height), power=0.7)
    try:
        return Image.composite(right_image, left_image, mask)
    finally:
        left_image.close()
        right_image.close()
        mask.close()

def get_poster_primary_color(image_path):
    try:
        with Image.open(image_path) as source_image:
            img = source_image.resize((100, 150), Image.Resampling.LANCZOS).convert('RGBA')
        pixels = list(img.getdata())
        filtered_pixels = [(r, g, b, 255) for r, g, b, a in pixels if a > 200 and not (r < 30 and g < 30 and b < 30) and not (r > 220 and g > 220 and b > 220)]
        if not filtered_pixels: filtered_pixels = [(p[0], p[1], p[2], 255) for p in pixels if p[3] > 100]
        if not filtered_pixels: return [(150, 100, 50, 255)]
        return Counter(filtered_pixels).most_common(10)
    except Exception:
        return [(150, 100, 50, 255)]

def create_blur_background(image_path, template_width, template_height, background_color, blur_size, color_ratio, lighten_gradient_strength=0.6):
    with Image.open(image_path) as source_image:
        original_img = source_image.convert('RGB')
    bg_img = ImageOps.fit(
        original_img,
        (template_width, template_height),
        method=Image.Resampling.LANCZOS,
    ).filter(ImageFilter.GaussianBlur(radius=int(blur_size)))
    original_img.close()

    actual_color = darken_color(background_color, 0.85)
    bg_color = actual_color[:3]
    blended_bg_img = blend_with_color(bg_img, bg_color, color_ratio).convert('RGBA')
    bg_img.close()

    if lighten_gradient_strength > 0:
        max_alpha = int(255 * max(0.0, min(1.0, float(lighten_gradient_strength))))
        gradient_mask = create_horizontal_gradient_mask((template_width, template_height), power=1.0)
        gradient_mask = gradient_mask.point([int((value / 255) * max_alpha) for value in range(256)])
        lighten_layer = Image.new("RGBA", (template_width, template_height), (255, 255, 255, 0))
        lighten_layer.putalpha(gradient_mask)
        gradient_mask.close()
        new_blended = Image.alpha_composite(blended_bg_img, lighten_layer)
        blended_bg_img.close()
        lighten_layer.close()
        blended_bg_img = new_blended

    out = add_film_grain(blended_bg_img, intensity=0.03)
    if out is not blended_bg_img:
        blended_bg_img.close()
    return out

def image_to_bytes(image):
    buffer = io.BytesIO()
    try:
        image.save(buffer, format="PNG", optimize=True)
        return buffer.getvalue()
    finally:
        buffer.close()
        image.close()

def create_style_multi_1(library_dir, title, font_path, font_size=(1,1), is_blur=False, blur_size=50, color_ratio=0.8, item_count=None, config=None):
    try:
        zh_font_size_ratio, en_font_size_ratio = font_size
        if int(blur_size) < 0: blur_size = 50
        if not (0 <= float(color_ratio) <= 1): color_ratio = 0.8
        if not float(zh_font_size_ratio) > 0: zh_font_size_ratio = 1
        if not float(en_font_size_ratio) > 0: en_font_size_ratio = 1

        zh_font_path, en_font_path = font_path
        title_zh, title_en = title

        poster_folder = Path(library_dir)
        first_image_path = poster_folder / "1.jpg"

        rows, cols, margin, corner_radius, rotation_angle, start_x, start_y, column_spacing = [POSTER_GEN_CONFIG[k] for k in ["ROWS", "COLS", "MARGIN", "CORNER_RADIUS", "ROTATION_ANGLE", "START_X", "START_Y", "COLUMN_SPACING"]]
        template_width, template_height = POSTER_GEN_CONFIG["CANVAS_WIDTH"], POSTER_GEN_CONFIG["CANVAS_HEIGHT"]

        with Image.open(first_image_path) as source_image:
            color_img = source_image.convert("RGB")
        vibrant_colors = find_dominant_vibrant_colors(color_img)
        soft_colors = [(237, 159, 77), (255, 183, 197), (186, 225, 255), (255, 223, 186), (202, 231, 200), (245, 203, 255)]

        if vibrant_colors:
            blur_color = vibrant_colors[0]
        else:
            blur_color = random.choice(soft_colors)

        base_color_for_badge = blur_color
        gradient_color = get_poster_primary_color(first_image_path)
        color_img.close()

        if is_blur:
          colored_bg_img = create_blur_background(first_image_path, template_width, template_height, blur_color, blur_size, color_ratio)
        else:
          colored_bg_img = create_gradient_background(template_width, template_height, gradient_color)

        supported_formats = (".jpg", ".jpeg", ".png", ".bmp", ".gif", ".webp")
        custom_order = "315426987"
        order_map = {num: index for index, num in enumerate(custom_order)}
        poster_files = sorted([os.path.join(poster_folder, f) for f in os.listdir(poster_folder) if os.path.isfile(os.path.join(poster_folder, f)) and f.lower().endswith(supported_formats) and os.path.splitext(f)[0] in order_map], key=lambda x: order_map[os.path.splitext(os.path.basename(x))[0]])

        if not poster_files: return False
        poster_files = poster_files[:rows * cols]
        cell_width, cell_height = POSTER_GEN_CONFIG["CELL_WIDTH"], POSTER_GEN_CONFIG["CELL_HEIGHT"]
        grouped_posters = [poster_files[i : i + rows] for i in range(0, len(poster_files), rows)]

        result = colored_bg_img.convert("RGBA")
        colored_bg_img.close()
        poster_group = Image.new("RGBA", result.size, (0, 0, 0, 0))
        for col_index, column_posters in enumerate(grouped_posters):
            if col_index >= cols: break
            column_x = start_x + col_index * column_spacing
            column_height = rows * cell_height + (rows - 1) * margin
            shadow_extra = 40
            column_image = Image.new("RGBA", (cell_width + shadow_extra, column_height + shadow_extra), (0, 0, 0, 0))
            for row_index, poster_path in enumerate(column_posters):
                try:
                    with Image.open(poster_path) as poster_source:
                        poster = ImageOps.fit(
                            poster_source.convert("RGB"),
                            (cell_width, cell_height),
                            method=Image.Resampling.LANCZOS,
                        )
                    if corner_radius > 0:
                        mask = Image.new("L", (cell_width, cell_height), 0)
                        ImageDraw.Draw(mask).rounded_rectangle([(0, 0), (cell_width, cell_height)], radius=corner_radius, fill=255)
                        poster_with_corners = Image.new("RGBA", poster.size, (0, 0, 0, 0))
                        poster_with_corners.paste(poster, (0, 0), mask)
                        poster.close()
                        mask.close()
                        poster = poster_with_corners
                    poster_with_shadow = add_shadow(
                        poster,
                        offset=(17, 14),
                        shadow_color=(0, 0, 0, 188),
                        blur_radius=16,
                    )
                    poster.close()
                    y_position = row_index * (cell_height + margin)
                    column_image.paste(poster_with_shadow, (0, y_position), poster_with_shadow)
                    poster_with_shadow.close()
                except Exception: continue

            rotation_canvas_size = int(math.sqrt((cell_width + shadow_extra) ** 2 + (column_height + shadow_extra) ** 2) * 1.5)
            rotation_canvas = Image.new("RGBA", (rotation_canvas_size, rotation_canvas_size), (0, 0, 0, 0))
            paste_x = (rotation_canvas_size - column_image.width) // 2
            paste_y = (rotation_canvas_size - column_image.height) // 2
            rotation_canvas.paste(column_image, (paste_x, paste_y), column_image)
            column_image.close()
            rotated_column = rotation_canvas.rotate(rotation_angle, Image.BICUBIC, expand=True)
            rotation_canvas.close()

            column_center_y = start_y + column_height // 2
            column_center_x = column_x
            if col_index == 1: column_center_x += cell_width - 50
            elif col_index == 2:
                column_center_y += -155
                column_center_x += (cell_width) * 2 - 40

            final_x = column_center_x - rotated_column.width // 2
            final_y = column_center_y - rotated_column.height // 2
            poster_group.paste(rotated_column, (final_x, final_y), rotated_column)
            rotated_column.close()

        poster_group_shadow = create_shadow_layer(
            poster_group,
            offset=(26, 22),
            shadow_color=(0, 0, 0, 76),
            blur_radius=28,
        )
        result = Image.alpha_composite(result, poster_group_shadow)
        poster_group_shadow.close()
        result = Image.alpha_composite(result, poster_group)
        poster_group.close()

        random_color = get_random_color(poster_files[0]) if poster_files else (random.randint(50, 200), random.randint(50, 200), random.randint(50, 200), 255)

        text_shadow_color = darken_color(blur_color, 0.8)
        prev = result
        result = draw_text_on_image(result, title_zh, (73.32, 427.34), zh_font_path, "ch.ttf", int(163 * float(zh_font_size_ratio)), shadow=is_blur, shadow_color=text_shadow_color)
        if result is not prev:
            prev.close()

        if title_en:
            base_font_size = 50 * float(en_font_size_ratio)
            line_spacing = base_font_size * 0.1
            words = title_en.split()
            word_count = len(words)
            max_chars_per_line = max(len(word) for word in words) if words else 0
            font_size = base_font_size * (10 / max(max_chars_per_line, word_count * 3)) ** 0.8 if max_chars_per_line > 10 or word_count > 3 else base_font_size
            font_size = max(font_size, 30)
            prev = result
            result, line_count = draw_multiline_text_on_image(result, title_en, (124.68, 624.55), en_font_path, "en.otf", int(font_size), line_spacing, shadow=is_blur, shadow_color=text_shadow_color)
            if result is not prev:
                prev.close()
            color_block_height = base_font_size + line_spacing + (line_count - 1) * (int(font_size) + line_spacing)
            result = draw_color_block(result, (84.38, 620.06), (21.51, color_block_height), random_color)

        if config and config.get("show_item_count", False) and item_count is not None:
            result = result.convert('RGBA')
            result = draw_badge(
                image=result, item_count=item_count, font_path=zh_font_path,
                style=config.get('badge_style', 'badge'),
                size_ratio=config.get('badge_size_ratio', 0.12),
                base_color=base_color_for_badge
            )

        return image_to_bytes(result)

    except Exception as e:
        logger.error(f"创建多图封面时出错: {e}", exc_info=True)
        return False
