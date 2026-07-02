import logging
import random
import base64
from pathlib import Path
from typing import Dict, Any, List, Optional

from app.emby_client import EmbyClient
from app.styles.style_single_1 import create_style_single_1
from app.styles.style_single_2 import create_style_single_2
from app.styles.style_multi_1 import create_style_multi_1

logger = logging.getLogger(__name__)

FONTS_DIR = Path(__file__).parent.parent / "fonts"

STYLE_NAMES = {
    "single_1": "单图风格 1",
    "single_2": "单图风格 2",
    "multi_1": "多图风格",
}


def resolve_font_path(config: dict) -> dict:
    fonts = {}
    for key, filename in [
        ("zh", "zh_font.ttf"),
        ("en", "en_font.ttf"),
        ("zh_multi", "zh_font_multi_1.ttf"),
        ("en_multi", "en_font_multi_1.otf"),
    ]:
        custom = config.get(f"{key.split('_')[0]}_font_path", "")
        if custom and Path(custom).exists():
            fonts[key] = custom
        else:
            fonts[key] = str(FONTS_DIR / filename)
    return fonts


def generate_cover_for_library(
    client: EmbyClient,
    library: Dict,
    config: dict,
) -> Dict:
    library_id = library.get("Id") or library.get("ItemId")
    library_name = library.get("Name", "Unknown")
    sort_by = config.get("sort_by", "Random")
    style = config.get("cover_style", "single_1")
    use_primary = config.get("use_primary", False)
    blur_size = int(config.get("blur_size", 50))
    color_ratio = float(config.get("color_ratio", 0.8))
    zh_font_size = float(config.get("zh_font_size", 1.0))
    en_font_size = float(config.get("en_font_size", 1.0))

    item_count = config.get("show_item_count", False)
    badge_config = None
    if item_count:
        badge_config = {
            "show_item_count": True,
            "badge_style": config.get("badge_style", "badge"),
            "badge_size_ratio": config.get("badge_size_ratio", 0.12),
        }

    fonts = resolve_font_path(config)

    try:
        multi_style = style.startswith("multi")
        required = 9 if multi_style else 1
        items = client.get_library_items(
            library_id=library_id,
            limit=required * 3,
            sort_by=sort_by,
        )
        if not items:
            return {"ok": False, "message": "媒体库没有可用的项目"}

        valid = []
        for item in items:
            img_url = client.get_image_url(item, use_primary)
            if img_url:
                valid.append((item, img_url))
            if len(valid) >= required:
                break

        if not valid:
            return {"ok": False, "message": "没有找到带图片的项目"}

        if sort_by == "Random":
            random.shuffle(valid)
        valid = valid[:required]

        tmp_dir = Path(f"/data/tmp/{library_id}")
        tmp_dir.mkdir(parents=True, exist_ok=True)

        image_paths = []
        for idx, (item, img_url) in enumerate(valid):
            ext = ".jpg"
            save_path = str(tmp_dir / f"{idx + 1}{ext}")
            result_path = client.download_image(img_url, save_path)
            if result_path:
                image_paths.append(result_path)

        if not image_paths:
            return {"ok": False, "message": "图片下载全部失败"}

        title_zh = library_name
        title_en = ""

        if style == "single_1":
            result = create_style_single_1(
                image_paths[0], (title_zh, title_en),
                (fonts["zh"], fonts["en"]),
                font_size=(zh_font_size, en_font_size),
                blur_size=blur_size, color_ratio=color_ratio,
                config=badge_config,
            )
        elif style == "single_2":
            result = create_style_single_2(
                image_paths[0], (title_zh, title_en),
                (fonts["zh"], fonts["en"]),
                font_size=(zh_font_size, en_font_size),
                blur_size=blur_size, color_ratio=color_ratio,
                config=badge_config,
            )
        elif style == "multi_1":
            lib_dir = tmp_dir / "multi"
            lib_dir.mkdir(parents=True, exist_ok=True)
            for i, p in enumerate(image_paths[:9]):
                import shutil
                target = lib_dir / f"{i + 1}.jpg"
                shutil.copy(p, target)
            for i in range(len(image_paths[:9]), 9):
                target = lib_dir / f"{i + 1}.jpg"
                if not target.exists() and image_paths:
                    import shutil
                    shutil.copy(image_paths[0], target)
            result = create_style_multi_1(
                str(lib_dir), (title_zh, title_en),
                (fonts["zh_multi"], fonts["en_multi"]),
                font_size=(zh_font_size, en_font_size),
                is_blur=config.get("multi_1_blur", False),
                blur_size=blur_size, color_ratio=color_ratio,
                config=badge_config,
            )
        else:
            return {"ok": False, "message": f"未知风格: {style}"}

        if not result:
            return {"ok": False, "message": "封面生成失败"}

        image_bytes = base64.b64decode(result)
        upload_ok = client.upload_library_image(library_id, image_bytes)

        if config.get("covers_output"):
            out_dir = Path(config["covers_output"])
            out_dir.mkdir(parents=True, exist_ok=True)
            out_path = out_dir / f"{library_name}.jpg"
            with open(out_path, "wb") as f:
                f.write(image_bytes)

        if upload_ok:
            return {"ok": True, "message": f"'{library_name}' 封面已更新"}
        else:
            return {"ok": True, "message": f"封面已生成但上传失败"}

    except Exception as e:
        logger.exception(f"为 '{library_name}' 生成封面失败")
        return {"ok": False, "message": str(e)}
