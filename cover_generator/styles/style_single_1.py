# services/cover_generator/styles/style_single_1.py

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

def adjust_color_macaron(color):
    h, s, v = rgb_to_hsv(color)
    target_saturation_range = (0.3, 0.7)
    target_value_range = (0.6, 0.85)
    if s < target_saturation_range[0]: s = target_saturation_range[0]
    elif s > target_saturation_range[1]: s = target_saturation_range[1]
    if v < target_value_range[0]: v = target_value_range[0]
    elif v > target_value_range[1]: v = target_value_range[1]
    return hsv_to_rgb(h, s, v)

def color_distance(color1, color2):
    h1, s1, v1 = rgb_to_hsv(color1)
    h2, s2, v2 = rgb_to_hsv(color2)
    h_dist = min(abs(h1 - h2), 1 - abs(h1 - h2))
    return h_dist * 5 + abs(s1 - s2) + abs(v1 - v2)

def find_dominant_macaron_colors(image, num_colors=5):
    img = image.copy()
    img.thumbnail((150, 150))
    img = img.convert('RGB')
    pixels = list(img.getdata())
    filtered_pixels = [p for p in pixels if is_not_black_white_gray_near(p)]
    if not filtered_pixels: return []
    color_counter = Counter(filtered_pixels)
    candidate_colors = color_counter.most_common(num_colors * 5)
    macaron_colors = []
    min_color_distance = 0.15
    for color, _ in candidate_colors:
        adjusted_color = adjust_color_macaron(color)
        if not any(color_distance(adjusted_color, existing) < min_color_distance for existing in macaron_colors):
            macaron_colors.append(adjusted_color)
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

def crop_to_square(img):
    width, height = img.size
    size = min(width, height)
    left = (width - size) // 2
    top = (height - size) // 2
    right = left + size
    bottom = top + size
    return img.crop((left, top, right, bottom))
    
def add_rounded_corners(img, radius=30):
    factor = 2
    width, height = img.size
    enlarged_img = img.resize((width * factor, height * factor), Image.Resampling.LANCZOS).convert("RGBA")
    mask = Image.new('L', (width * factor, height * factor), 0)
    draw = ImageDraw.Draw(mask)
    draw.rounded_rectangle([(0, 0), (width * factor, height * factor)], radius=radius * factor, fill=255)
    background = Image.new("RGBA", (width * factor, height * factor), (255, 255, 255, 0))
    high_res_result = Image.composite(enlarged_img, background, mask)
    return high_res_result.resize((width, height), Image.Resampling.LANCZOS)

def add_shadow_and_rotate(canvas, img, angle, offset=(10, 10), radius=10, opacity=0.5, center_pos=None):
    width, height = img.size
    if center_pos is None: center_pos = (canvas.width // 2, canvas.height // 2)
    padding = max(radius * 4, 100)
    shadow_size = (width + padding * 2, height + padding * 2)
    shadow = Image.new("RGBA", shadow_size, (0, 0, 0, 0))
    shadow_mask = img.split()[3] if img.mode == "RGBA" else Image.new("L", (width, height), 255)
    shadow_center = (padding, padding)
    shadow.paste((0, 0, 0, int(255 * opacity)), (shadow_center[0], shadow_center[1], shadow_center[0] + width, shadow_center[1] + height), shadow_mask)
    shadow = shadow.filter(ImageFilter.GaussianBlur(radius))
    rotated_shadow = shadow.rotate(angle, Image.BICUBIC, expand=True)
    shadow_width, shadow_height = rotated_shadow.size
    shadow_x = center_pos[0] - shadow_width // 2 + offset[0]
    shadow_y = center_pos[1] - shadow_height // 2 + offset[1]
    canvas.paste(rotated_shadow, (shadow_x, shadow_y), rotated_shadow)
    rotated_img = img.rotate(angle, Image.BICUBIC, expand=True)
    img_width, img_height = rotated_img.size
    img_x = center_pos[0] - img_width // 2
    img_y = center_pos[1] - img_height // 2
    canvas.paste(rotated_img, (img_x, img_y), rotated_img)
    return canvas

def image_to_base64(image):
    buffer = BytesIO()
    image.save(buffer, format="PNG", optimize=True)
    return base64.b64encode(buffer.getvalue()).decode('utf-8')

# ========== 主函数 ==========
def create_style_single_1(image_path, title, font_path, font_size=(1,1), blur_size=50, color_ratio=0.8, item_count=None, config=None):
    try:
        zh_font_path, en_font_path = font_path
        title_zh, title_en = title
        zh_font_size_ratio, en_font_size_ratio = font_size

        if int(blur_size) < 0: blur_size = 50
        if not (0 <= float(color_ratio) <= 1): color_ratio = 0.8
        if not float(zh_font_size_ratio) > 0: zh_font_size_ratio = 1
        if not float(en_font_size_ratio) > 0: en_font_size_ratio = 1
        
        num_colors = 6
        original_img = Image.open(image_path).convert("RGB")
        
        candidate_colors = find_dominant_macaron_colors(original_img, num_colors=num_colors)
        random.shuffle(candidate_colors)
        extracted_colors = candidate_colors[:num_colors]
            
        soft_macaron_colors = [(237, 159, 77), (186, 225, 255), (255, 223, 186), (202, 231, 200)]
        
        while len(extracted_colors) < num_colors:
            if not extracted_colors:
                extracted_colors.append(random.choice(soft_macaron_colors))
            else:
                best_color = max(soft_macaron_colors, key=lambda color: min(color_distance(color, existing) for existing in extracted_colors))
                extracted_colors.append(best_color)
        
        # ==================== 可微调旋钮 (颜色) ====================
        # `darken_color` 的第二个参数 (0.0 - 1.0) 控制背景色的深浅。值越小，颜色越深。
        # 推荐范围: 0.7 - 0.9
        bg_color = darken_color(extracted_colors[0], 0.85)
        base_color_for_badge = extracted_colors[0]
        # ==========================================================
        card_colors = [extracted_colors[1], extracted_colors[2]]
        
        bg_img = ImageOps.fit(original_img.copy(), canvas_size, method=Image.LANCZOS).filter(ImageFilter.GaussianBlur(radius=int(blur_size)))
        bg_img_array = np.array(bg_img, dtype=float)
        bg_color_array = np.array([[bg_color]], dtype=float)
        blended_bg = np.clip(bg_img_array * (1 - float(color_ratio)) + bg_color_array * float(color_ratio), 0, 255).astype(np.uint8)
        blended_bg_img = Image.fromarray(blended_bg)
        
        # ==================== 可微调旋钮 (颗粒) ====================
        # `add_film_grain` 的 intensity 参数 (0.0 - 1.0) 控制颗粒强度。值越大，颗粒越明显。
        # 推荐范围: 0.02 - 0.05
        blended_bg_img = add_film_grain(blended_bg_img, intensity=0.03)
        # ==========================================================
        
        canvas = Image.new("RGBA", canvas_size, (0, 0, 0, 0))
        canvas.paste(blended_bg_img)
        
        square_img = crop_to_square(original_img)
        card_size = int(canvas_size[1] * 0.7)
        square_img = square_img.resize((card_size, card_size), Image.LANCZOS)
        
        main_card = add_rounded_corners(square_img, radius=card_size//8).convert("RGBA")
        
        aux_card1_bg = square_img.copy().filter(ImageFilter.GaussianBlur(radius=8))
        aux_card1_array = np.array(aux_card1_bg, dtype=float)
        card_color1_array = np.array([[card_colors[0]]], dtype=float)
        blended_card1 = np.clip(aux_card1_array * 0.5 + card_color1_array * 0.5, 0, 255).astype(np.uint8)
        aux_card1 = add_rounded_corners(Image.fromarray(blended_card1), radius=card_size//8).convert("RGBA")
        
        aux_card2_bg = square_img.copy().filter(ImageFilter.GaussianBlur(radius=16))
        aux_card2_array = np.array(aux_card2_bg, dtype=float)
        card_color2_array = np.array([[card_colors[1]]], dtype=float)
        blended_card2 = np.clip(aux_card2_array * 0.4 + card_color2_array * 0.6, 0, 255).astype(np.uint8)
        aux_card2 = add_rounded_corners(Image.fromarray(blended_card2), radius=card_size//8).convert("RGBA")
        
        center_pos = (int(canvas_size[0] - canvas_size[1] * 0.5), int(canvas_size[1] * 0.5))
        rotation_angles = [36, 18, 0]
        shadow_configs = [{'offset': (10, 16), 'radius': 12, 'opacity': 0.4}, {'offset': (15, 22), 'radius': 15, 'opacity': 0.5}, {'offset': (20, 26), 'radius': 18, 'opacity': 0.6}]
        
        cards_canvas = Image.new("RGBA", canvas_size, (0, 0, 0, 0))
        for card, angle, shadow_config in zip([aux_card2, aux_card1, main_card], rotation_angles, shadow_configs):
            cards_canvas = add_shadow_and_rotate(cards_canvas, card, angle, **shadow_config, center_pos=center_pos)
        
        canvas = Image.alpha_composite(canvas.convert("RGBA"), cards_canvas)
        
        text_layer = Image.new('RGBA', canvas_size, (255, 255, 255, 0))
        shadow_layer = Image.new("RGBA", canvas_size, (0, 0, 0, 0))
        shadow_draw = ImageDraw.Draw(shadow_layer)
        draw = ImageDraw.Draw(text_layer)
        
        left_area_center_x = int(canvas_size[0] * 0.25)
        left_area_center_y = canvas_size[1] // 2
        zh_font_size = int(canvas_size[1] * 0.17 * float(zh_font_size_ratio))
        en_font_size = int(canvas_size[1] * 0.07 * float(en_font_size_ratio))
        zh_font = ImageFont.truetype(zh_font_path, zh_font_size)
        en_font = ImageFont.truetype(en_font_path, en_font_size)
        
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
        combined = Image.alpha_composite(canvas, blurred_shadow)
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
        logger.error(f"创建单图封面(style 1)时出错: {e}", exc_info=True)
        return False