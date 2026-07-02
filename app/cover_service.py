import base64
import logging
import random
import re
import shutil
from pathlib import Path
from typing import Any, Dict, Optional, Tuple

from app.emby_client import EmbyClient
from app.styles.style_multi_1 import create_style_multi_1
from app.styles.style_single_1 import create_style_single_1
from app.styles.style_single_2 import create_style_single_2

logger = logging.getLogger(__name__)

FONTS_DIR = Path(__file__).parent.parent / "fonts"

STYLE_NAMES = {
    "single_1": "单图风格 1",
    "single_2": "单图风格 2",
    "multi_1": "多图风格 1",
}

POSTER_FILENAMES = ("poster.jpg", "poster.jpeg", "poster.png", "poster.webp")


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


def _num(value: Any, default: float, *, as_int: bool = False) -> float | int:
    try:
        number = float(value)
    except (TypeError, ValueError):
        number = default
    return int(number) if as_int else number


def _sanitize_name(name: str) -> str:
    safe = re.sub(r"[\\/:*?\"<>|]+", "_", name or "library")
    safe = safe.strip().strip(".")
    return safe or "library"


def _get_style_settings(style: str, config: dict) -> dict:
    if style == "multi_1":
        return {
            "use_primary": bool(config.get("multi_1_use_primary", True)),
            "blur_size": int(_num(config.get("multi_blur_size"), 50, as_int=True)),
            "color_ratio": float(_num(config.get("multi_color_ratio"), 0.8)),
            "zh_font_size": float(_num(config.get("multi_zh_font_size"), 1.0)),
            "en_font_size": float(_num(config.get("multi_en_font_size"), 1.0)),
            "show_item_count": bool(config.get("multi_show_item_count", False)),
            "badge_style": config.get("multi_badge_style", "badge"),
            "badge_size_ratio": float(_num(config.get("multi_badge_size_ratio"), 0.12)),
            "is_blur": bool(config.get("multi_1_blur", False)),
        }

    return {
        "use_primary": bool(config.get("single_use_primary", True)),
        "blur_size": int(_num(config.get("single_blur_size"), 50, as_int=True)),
        "color_ratio": float(_num(config.get("single_color_ratio"), 0.8)),
        "zh_font_size": float(_num(config.get("single_zh_font_size"), 1.0)),
        "en_font_size": float(_num(config.get("single_en_font_size"), 1.0)),
        "show_item_count": bool(config.get("single_show_item_count", False)),
        "badge_style": config.get("single_badge_style", "badge"),
        "badge_size_ratio": float(_num(config.get("single_badge_size_ratio"), 0.12)),
        "is_blur": False,
    }


def _resolve_library_titles(library: Dict, config: dict) -> Tuple[str, str]:
    library_id = str(library.get("Id") or library.get("ItemId") or "")
    library_name = library.get("Name", "Unknown")

    if not config.get("custom_library_titles_enabled"):
        return library_name, ""

    overrides = config.get("library_title_overrides") or {}
    override = overrides.get(library_id)

    if not override:
        for key, value in overrides.items():
            if key == library_name:
                override = value
                break

    if not isinstance(override, dict):
        return library_name, ""

    title_zh = str(override.get("zh", "") or "").strip() or library_name
    title_en = str(override.get("en", "") or "").strip()
    return title_zh, title_en


def _local_poster_candidates(item: Dict) -> list[Path]:
    item_path_raw = item.get("Path")
    if not item_path_raw:
        return []

    item_path = Path(item_path_raw)
    search_dirs = []

    if item_path.is_dir():
        search_dirs.extend([item_path, item_path.parent, item_path.parent.parent])
    else:
        search_dirs.extend([item_path.parent, item_path.parent.parent, item_path.parent.parent.parent])

    seen = set()
    candidates: list[Path] = []
    for directory in search_dirs:
        if not directory or str(directory) in seen:
            continue
        seen.add(str(directory))
        if not directory.exists() or not directory.is_dir():
            continue
        for filename in POSTER_FILENAMES:
            candidate = directory / filename
            if candidate.exists() and candidate.is_file():
                candidates.append(candidate)
    return candidates


def _copy_local_poster(item: Dict, save_path: Path) -> Optional[str]:
    for candidate in _local_poster_candidates(item):
        try:
            shutil.copy(candidate, save_path)
            logger.info(f"命中本地海报: {candidate}")
            return str(save_path)
        except Exception as exc:
            logger.warning(f"复制本地海报失败 {candidate}: {exc}")
    return None


def _prepare_source_dir(config: dict, library_name: str, library_id: str) -> Path:
    root = Path(config.get("covers_input") or "/data/input")
    source_dir = root / _sanitize_name(library_name) / library_id
    source_dir.mkdir(parents=True, exist_ok=True)
    return source_dir


def generate_cover_for_library(
    client: EmbyClient,
    library: Dict,
    config: dict,
) -> Dict:
    library_id = str(library.get("Id") or library.get("ItemId") or "")
    library_name = library.get("Name", "Unknown")
    sort_by = config.get("sort_by") or "Random"
    style = config.get("cover_style") or "single_1"
    settings = _get_style_settings(style, config)

    badge_config = None
    item_count = library.get("ChildCount") or library.get("RecursiveItemCount")
    if settings["show_item_count"]:
        badge_config = {
            "show_item_count": True,
            "badge_style": settings["badge_style"],
            "badge_size_ratio": settings["badge_size_ratio"],
        }

    fonts = resolve_font_path(config)

    try:
        multi_style = style.startswith("multi")
        required = 9 if multi_style else 1
        items = client.get_library_items(
            library_id=library_id,
            limit=required * 4,
            sort_by=sort_by,
        )
        if not items:
            return {"ok": False, "message": "媒体库没有可用的项目"}

        valid = []
        for item in items:
            has_local_poster = bool(_local_poster_candidates(item))
            img_url = client.get_image_url(item, settings["use_primary"])
            if has_local_poster or img_url:
                valid.append((item, img_url))
            if len(valid) >= required:
                break

        if not valid:
            return {"ok": False, "message": "没有找到可用的本地 poster.jpg 或远程图片"}

        if sort_by == "Random":
            random.shuffle(valid)
        valid = valid[:required]

        source_dir = _prepare_source_dir(config, library_name, library_id)
        image_paths = []

        for idx, (item, img_url) in enumerate(valid):
            save_path = source_dir / f"{idx + 1}.jpg"
            result_path = _copy_local_poster(item, save_path)
            if not result_path and img_url:
                result_path = client.download_image(img_url, str(save_path))
            if result_path:
                image_paths.append(result_path)

        if not image_paths:
            return {"ok": False, "message": "图片下载全部失败"}

        title_zh, title_en = _resolve_library_titles(library, config)

        if style == "single_1":
            result = create_style_single_1(
                image_paths[0],
                (title_zh, title_en),
                (fonts["zh"], fonts["en"]),
                font_size=(settings["zh_font_size"], settings["en_font_size"]),
                blur_size=settings["blur_size"],
                color_ratio=settings["color_ratio"],
                item_count=item_count,
                config=badge_config,
            )
        elif style == "single_2":
            result = create_style_single_2(
                image_paths[0],
                (title_zh, title_en),
                (fonts["zh"], fonts["en"]),
                font_size=(settings["zh_font_size"], settings["en_font_size"]),
                blur_size=settings["blur_size"],
                color_ratio=settings["color_ratio"],
                item_count=item_count,
                config=badge_config,
            )
        elif style == "multi_1":
            lib_dir = source_dir / "multi"
            lib_dir.mkdir(parents=True, exist_ok=True)
            for i, path in enumerate(image_paths[:9]):
                target = lib_dir / f"{i + 1}.jpg"
                shutil.copy(path, target)
            for i in range(len(image_paths[:9]), 9):
                target = lib_dir / f"{i + 1}.jpg"
                if image_paths:
                    shutil.copy(image_paths[0], target)
            result = create_style_multi_1(
                str(lib_dir),
                (title_zh, title_en),
                (fonts["zh_multi"], fonts["en_multi"]),
                font_size=(settings["zh_font_size"], settings["en_font_size"]),
                is_blur=settings["is_blur"],
                blur_size=settings["blur_size"],
                color_ratio=settings["color_ratio"],
                item_count=item_count,
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
            out_path = out_dir / f"{_sanitize_name(library_name)}.jpg"
            with open(out_path, "wb") as f:
                f.write(image_bytes)

        if upload_ok:
            return {
                "ok": True,
                "message": f"'{library_name}' 封面已更新",
                "style_name": STYLE_NAMES.get(style, style),
            }
        return {
            "ok": True,
            "message": "封面已生成但上传失败",
            "style_name": STYLE_NAMES.get(style, style),
        }

    except Exception as e:
        logger.exception(f"为 '{library_name}' 生成封面失败")
        return {"ok": False, "message": str(e)}
