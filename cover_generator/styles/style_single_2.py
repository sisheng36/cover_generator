# services/cover_generator/styles/style_single_2.py

import logging
import colorsys
import random
import base64
from io import BytesIO
from collections import Counter
import numpy as np
from PIL import Image, ImageDraw, ImageFont, ImageFilter, ImageOps

from .badge_drawer import draw_badge

logger = logging.getLogger(__name__)

# ========== 配置 ==========
canvas_size = (1920, 1080)

# ========== 辅助函数 ==========
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

def add_film_grain(image, intensity=0.05):
    img_array = np.array(image)
    noise = np.random.normal(0, intensity * 255, img_array.shape)
    img_array = np.clip(img_array + noise, 0, 255).astype(np.uint8)
    return Image.fromarray(img_array)

def align_image_right(img, canvas_size):
    canvas_width, canvas_height = canvas_size
    target_width = int(canvas_width * 0.675)
    img_width, img_height = img.size
    scale_factor = canvas_height / img_height
    new_img_width = int(img_width * scale_factor)
    resized_img = img.resize((new_img_width, canvas_height), Image.LANCZOS)
    if new_img_width < target_width:
        scale_factor = target_width / img_width
        new_img_height = int(img_height * scale_factor)
        resized_img = img.resize((target_width, new_img_height), Image.LANCZOS)
        if new_img_height > canvas_height:
            crop_top = (new_img_height - canvas_height) // 2
            resized_img = resized_img.crop((0, crop_top, target_width, crop_top + canvas_height))
        final_img = Image.new("RGB", canvas_size)
        final_img.paste(resized_img, (canvas_width - target_width, 0))
        return final_img
    resized_img_center_x = new_img_width / 2
    crop_left = max(0, resized_img_center_x - target_width / 2)
    if crop_left + target_width > new_img_width:
        crop_left = new_img_width - target_width
    crop_right = crop_left + target_width
    crop_left = max(0, crop_left)
    crop_right = min(new_img_width, crop_right)
    cropped_img = resized_img.crop((int(crop_left), 0, int(crop_right), canvas_height))
    final_img = Image.new("RGB", canvas_size)
    paste_x = canvas_width - cropped_img.width + int(canvas_width * 0.075)
    final_img.paste(cropped_img, (paste_x, 0))
    return final_img

def create_diagonal_mask(size, split_top=0.5, split_bottom=0.33):
    mask = Image.new('L', size, 255)
    draw = ImageDraw.Draw(mask)
    width, height = size
    top_x = int(width * split_top)
    bottom_x = int(width * split_bottom)
    draw.polygon([(top_x, 0), (width, 0), (width, height), (bottom_x, height)], fill=0)
    draw.polygon([(0, 0), (top_x, 0), (bottom_x, height), (0, height)], fill=255)
    return mask

def create_shadow_mask(size, split_top=0.5, split_bottom=0.33, feather_size=40):
    width, height = size
    top_x = int(width * split_top)
    bottom_x = int(width * split_bottom)
    mask = Image.new('L', size, 0)
    draw = ImageDraw.Draw(mask)
    shadow_width = feather_size // 3
    draw.polygon([(top_x - 5, 0), (top_x - 5 + shadow_width, 0), (bottom_x - 5 + shadow_width, height), (bottom_x - 5, height)], fill=255)
    return mask.filter(ImageFilter.GaussianBlur(radius=feather_size//3))

def image_to_base64(image):
    buffer = BytesIO()
    image.save(buffer, format="PNG", optimize=True)
    return base64.b64encode(buffer.getvalue()).decode('utf-8')

# ========== 主函数 ==========
def create_style_single_2(image_path, title, font_path, font_size=(1,1), blur_size=50, color_ratio=0.8, item_count=None, config=None):
    try:
        zh_font_path, en_font_path = font_path
        title_zh, title_en = title
        zh_font_size_ratio, en_font_size_ratio = font_size

        if int(blur_size) < 0: blur_size = 50
        if not (0 <= float(color_ratio) <= 1): color_ratio = 0.8
        if not float(zh_font_size_ratio) > 0: zh_font_size_ratio = 1
        if not float(en_font_size_ratio) > 0: en_font_size_ratio = 1

        split_top = 0.55
        split_bottom = 0.4
        
        fg_img_original = Image.open(image_path).convert("RGB")
        fg_img = align_image_right(fg_img_original, canvas_size)
        
        vibrant_colors = find_dominant_vibrant_colors(fg_img)
        soft_colors = [(237, 159, 77), (255, 183, 197), (186, 225, 255), (255, 223, 186), (202, 231, 200), (245, 203, 255)]
        
        if vibrant_colors:
            bg_color = vibrant_colors[0]
        else:
            bg_color = random.choice(soft_colors)
        
        base_color_for_badge = bg_color

        # ==================== 可微调旋鈕 (阴影颜色) ====================
        # `darken_color` 的第二个参数 (0.0 - 1.0) 控制斜线分割处的阴影深浅。值越小，阴影越深。
        # 推荐范围: 0.4 - 0.6
        shadow_color = darken_color(bg_color, 0.5)
        # ===========================================================
        
        bg_img_original = Image.open(image_path).convert("RGB")
        bg_img = ImageOps.fit(bg_img_original, canvas_size, method=Image.LANCZOS).filter(ImageFilter.GaussianBlur(radius=int(blur_size)))

        # ==================== 可微调旋鈕 (背景颜色) ====================
        # `darken_color` 的第二个参数 (0.0 - 1.0) 控制背景主色调的深浅。值越小，颜色越深。
        # 推荐范围: 0.7 - 0.9
        bg_color = darken_color(bg_color, 0.85)
        # ===========================================================
        
        bg_img_array = np.array(bg_img, dtype=float)
        bg_color_array = np.array([[bg_color]], dtype=float)
        blended_bg = np.clip(bg_img_array * (1 - float(color_ratio)) + bg_color_array * float(color_ratio), 0, 255).astype(np.uint8)
        blended_bg_img = Image.fromarray(blended_bg)
        
        # ==================== 可微调旋鈕 (颗粒) ====================
        # `add_film_grain` 的 intensity 参数 (0.0 - 1.0) 控制颗粒强度。值越大，颗粒越明显。
        # 推荐范围: 0.02 - 0.05
        blended_bg_img = add_film_grain(blended_bg_img, intensity=0.05)
        # ==========================================================
        
        diagonal_mask = create_diagonal_mask(canvas_size, split_top, split_bottom)
        canvas = fg_img.copy()
        shadow_mask = create_shadow_mask(canvas_size, split_top, split_bottom, feather_size=30)
        shadow_layer = Image.new('RGB', canvas_size, shadow_color)
        temp_canvas = Image.new('RGB', canvas_size)
        temp_canvas.paste(canvas)
        temp_canvas.paste(shadow_layer, mask=shadow_mask)
        canvas = Image.composite(blended_bg_img, temp_canvas, diagonal_mask)
        
        canvas_rgba = canvas.convert('RGBA')
        text_layer = Image.new('RGBA', canvas_size, (255, 255, 255, 0))
        shadow_layer = Image.new("RGBA", canvas_size, (0, 0, 0, 0))
        shadow_draw = ImageDraw.Draw(shadow_layer)
        draw = ImageDraw.Draw(text_layer)   
        
        left_area_center_x = int(canvas_size[0] * 0.25)
        left_area_center_y = canvas_size[1] // 2
        zh_font_size = int(canvas_size[1] * 0.17 * float(zh_font_size_ratio))
        en_font_size = int(canvas_size[1] * 0.07 * float(en_font_size_ratio))
        zh_font = ImageFont.truetype(str(zh_font_path), zh_font_size)
        en_font = ImageFont.truetype(str(en_font_path), en_font_size)
        
        text_color = (255, 255, 255, 229)
        text_shadow_color = darken_color(bg_color, 0.8) + (75,)
        shadow_offset = 12
        
        zh_bbox = draw.textbbox((0, 0), title_zh, font=zh_font)
        zh_x = left_area_center_x - (zh_bbox[2] - zh_bbox[0]) // 2
        zh_y = left_area_center_y - (zh_bbox[3] - zh_bbox[1]) - en_font_size // 2 - 5
        
        for offset in range(3, shadow_offset + 1, 2):
            shadow_draw.text((zh_x + offset, zh_y + offset), title_zh, font=zh_font, fill=text_shadow_color)
        draw.text((zh_x, zh_y), title_zh, font=zh_font, fill=text_color)
        
        if title_en:
            en_bbox = draw.textbbox((0, 0), title_en, font=en_font)
            en_x = left_area_center_x - (en_bbox[2] - en_bbox[0]) // 2
            en_y = zh_y + (zh_bbox[3] - zh_bbox[1]) + en_font_size
            for offset in range(2, shadow_offset // 2 + 1):
                shadow_draw.text((en_x + offset, en_y + offset), title_en, font=en_font, fill=text_shadow_color)
            draw.text((en_x, en_y), title_en, font=en_font, fill=text_color)

        blurred_shadow = shadow_layer.filter(ImageFilter.GaussianBlur(radius=shadow_offset))
        combined = Image.alpha_composite(canvas_rgba, blurred_shadow)
        combined = Image.alpha_composite(combined, text_layer)

        if config and config.get("show_item_count", False) and item_count is not None:
            combined = combined.convert('RGBA')
            combined = draw_badge(
                image=combined, item_count=item_count, font_path=zh_font_path,
                style=config.get('badge_style', 'badge'),
                size_ratio=config.get('badge_size_ratio', 0.12),
                base_color=base_color_for_badge
            )

        return image_to_base64(combined)
        
    except Exception as e:
        logger.error(f"创建单图封面(style 2)时出错: {e}", exc_info=True)
        return False