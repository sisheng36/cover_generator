import json
import logging
from pathlib import Path
from typing import Any, Dict

logger = logging.getLogger(__name__)

CONFIG_FILE = Path("/data/config.json")

DEFAULT_CONFIG: Dict[str, Any] = {
    "emby_server_url": "",
    "emby_api_key": "",
    "selected_libraries": [],
    "cover_style": "single_1",
    "sort_by": "Random",
    "covers_input": "/data/input",
    "covers_output": "/data/covers_output",
    "custom_library_titles_enabled": False,
    "library_title_overrides": {},
    "single_use_primary": True,
    "single_blur_size": 50,
    "single_color_ratio": 0.8,
    "single_zh_font_size": 1.0,
    "single_en_font_size": 1.0,
    "single_show_item_count": False,
    "single_badge_style": "badge",
    "single_badge_size_ratio": 0.12,
    "multi_1_blur": False,
    "multi_1_use_primary": True,
    "multi_blur_size": 50,
    "multi_color_ratio": 0.8,
    "multi_zh_font_size": 1.0,
    "multi_en_font_size": 1.0,
    "multi_show_item_count": False,
    "multi_badge_style": "badge",
    "multi_badge_size_ratio": 0.12,
    "notification_enabled": False,
    "tg_token": "",
    "tg_chat_id": "",
    "tmdb_api_key": "",
    "notify_types": [],
    "aggregate_enabled": True,
    "aggregate_time": 15,
    "scheduler_enabled": False,
    "scheduler_cron": "0 4 * * *",
    "scheduled_libraries": [],
}


def _coerce_bool(value: Any, default: bool) -> bool:
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        lowered = value.strip().lower()
        if lowered in {"1", "true", "yes", "on"}:
            return True
        if lowered in {"0", "false", "no", "off"}:
            return False
    if value is None:
        return default
    return bool(value)


def _coerce_number(value: Any, default: float, *, as_int: bool = False) -> Any:
    if value in ("", None):
        return default
    try:
        num = float(value)
    except (TypeError, ValueError):
        return default
    return int(num) if as_int else num


def _coerce_list(value: Any) -> list:
    if isinstance(value, list):
        return value
    if value in ("", None):
        return []
    return [value]


def _coerce_title_overrides(value: Any) -> Dict[str, Dict[str, str]]:
    if not isinstance(value, dict):
        return {}

    normalized: Dict[str, Dict[str, str]] = {}
    for key, item in value.items():
        if not isinstance(item, dict):
            continue
        normalized[str(key)] = {
            "zh": str(item.get("zh", "") or "").strip(),
            "en": str(item.get("en", "") or "").strip(),
        }
    return normalized


def normalize_config(raw_config: Dict[str, Any] | None) -> Dict[str, Any]:
    raw = dict(raw_config or {})
    config = DEFAULT_CONFIG.copy()
    config.update(raw)

    if "single_blur_size" not in raw:
        config["single_blur_size"] = _coerce_number(raw.get("blur_size"), DEFAULT_CONFIG["single_blur_size"], as_int=True)
    if "single_color_ratio" not in raw:
        config["single_color_ratio"] = _coerce_number(raw.get("color_ratio"), DEFAULT_CONFIG["single_color_ratio"])
    if "single_zh_font_size" not in raw:
        config["single_zh_font_size"] = _coerce_number(raw.get("zh_font_size"), DEFAULT_CONFIG["single_zh_font_size"])
    if "single_en_font_size" not in raw:
        config["single_en_font_size"] = _coerce_number(raw.get("en_font_size"), DEFAULT_CONFIG["single_en_font_size"])
    if "single_show_item_count" not in raw:
        config["single_show_item_count"] = _coerce_bool(raw.get("show_item_count"), DEFAULT_CONFIG["single_show_item_count"])
    if "single_badge_style" not in raw:
        config["single_badge_style"] = str(raw.get("badge_style") or DEFAULT_CONFIG["single_badge_style"])
    if "single_badge_size_ratio" not in raw:
        config["single_badge_size_ratio"] = _coerce_number(
            raw.get("badge_size_ratio"),
            DEFAULT_CONFIG["single_badge_size_ratio"],
        )

    if "multi_blur_size" not in raw:
        config["multi_blur_size"] = _coerce_number(raw.get("blur_size_multi_1"), DEFAULT_CONFIG["multi_blur_size"], as_int=True)
    if "multi_color_ratio" not in raw:
        config["multi_color_ratio"] = _coerce_number(raw.get("color_ratio_multi_1"), DEFAULT_CONFIG["multi_color_ratio"])
    if "multi_zh_font_size" not in raw:
        config["multi_zh_font_size"] = _coerce_number(raw.get("zh_font_size_multi_1"), DEFAULT_CONFIG["multi_zh_font_size"])
    if "multi_en_font_size" not in raw:
        config["multi_en_font_size"] = _coerce_number(raw.get("en_font_size_multi_1"), DEFAULT_CONFIG["multi_en_font_size"])
    if "multi_show_item_count" not in raw:
        config["multi_show_item_count"] = _coerce_bool(raw.get("show_item_count"), DEFAULT_CONFIG["multi_show_item_count"])
    if "multi_badge_style" not in raw:
        config["multi_badge_style"] = str(raw.get("badge_style") or DEFAULT_CONFIG["multi_badge_style"])
    if "multi_badge_size_ratio" not in raw:
        config["multi_badge_size_ratio"] = _coerce_number(
            raw.get("badge_size_ratio"),
            DEFAULT_CONFIG["multi_badge_size_ratio"],
        )

    if "single_use_primary" not in raw:
        config["single_use_primary"] = _coerce_bool(
            raw.get("use_primary"),
            DEFAULT_CONFIG["single_use_primary"],
        )
    if "multi_1_use_primary" not in raw:
        config["multi_1_use_primary"] = _coerce_bool(
            raw.get("use_primary"),
            DEFAULT_CONFIG["multi_1_use_primary"],
        )

    config["emby_server_url"] = str(config.get("emby_server_url") or "").strip()
    config["emby_api_key"] = str(config.get("emby_api_key") or "").strip()
    config["cover_style"] = str(config.get("cover_style") or DEFAULT_CONFIG["cover_style"])
    config["sort_by"] = str(config.get("sort_by") or DEFAULT_CONFIG["sort_by"])
    config["covers_input"] = str(config.get("covers_input") or DEFAULT_CONFIG["covers_input"]).strip()
    config["covers_output"] = str(config.get("covers_output") or DEFAULT_CONFIG["covers_output"]).strip()

    config["custom_library_titles_enabled"] = _coerce_bool(
        config.get("custom_library_titles_enabled"),
        DEFAULT_CONFIG["custom_library_titles_enabled"],
    )
    config["library_title_overrides"] = _coerce_title_overrides(config.get("library_title_overrides"))

    config["selected_libraries"] = [str(item) for item in _coerce_list(config.get("selected_libraries")) if str(item).strip()]
    config["scheduled_libraries"] = [str(item) for item in _coerce_list(config.get("scheduled_libraries")) if str(item).strip()]
    config["notify_types"] = [str(item) for item in _coerce_list(config.get("notify_types")) if str(item).strip()]

    config["single_use_primary"] = _coerce_bool(config.get("single_use_primary"), DEFAULT_CONFIG["single_use_primary"])
    config["single_blur_size"] = _coerce_number(config.get("single_blur_size"), DEFAULT_CONFIG["single_blur_size"], as_int=True)
    config["single_color_ratio"] = _coerce_number(config.get("single_color_ratio"), DEFAULT_CONFIG["single_color_ratio"])
    config["single_zh_font_size"] = _coerce_number(config.get("single_zh_font_size"), DEFAULT_CONFIG["single_zh_font_size"])
    config["single_en_font_size"] = _coerce_number(config.get("single_en_font_size"), DEFAULT_CONFIG["single_en_font_size"])
    config["single_show_item_count"] = _coerce_bool(
        config.get("single_show_item_count"),
        DEFAULT_CONFIG["single_show_item_count"],
    )
    config["single_badge_style"] = str(config.get("single_badge_style") or DEFAULT_CONFIG["single_badge_style"])
    config["single_badge_size_ratio"] = _coerce_number(
        config.get("single_badge_size_ratio"),
        DEFAULT_CONFIG["single_badge_size_ratio"],
    )

    config["multi_1_blur"] = _coerce_bool(config.get("multi_1_blur"), DEFAULT_CONFIG["multi_1_blur"])
    config["multi_1_use_primary"] = _coerce_bool(
        config.get("multi_1_use_primary"),
        DEFAULT_CONFIG["multi_1_use_primary"],
    )
    config["multi_blur_size"] = _coerce_number(config.get("multi_blur_size"), DEFAULT_CONFIG["multi_blur_size"], as_int=True)
    config["multi_color_ratio"] = _coerce_number(config.get("multi_color_ratio"), DEFAULT_CONFIG["multi_color_ratio"])
    config["multi_zh_font_size"] = _coerce_number(config.get("multi_zh_font_size"), DEFAULT_CONFIG["multi_zh_font_size"])
    config["multi_en_font_size"] = _coerce_number(config.get("multi_en_font_size"), DEFAULT_CONFIG["multi_en_font_size"])
    config["multi_show_item_count"] = _coerce_bool(
        config.get("multi_show_item_count"),
        DEFAULT_CONFIG["multi_show_item_count"],
    )
    config["multi_badge_style"] = str(config.get("multi_badge_style") or DEFAULT_CONFIG["multi_badge_style"])
    config["multi_badge_size_ratio"] = _coerce_number(
        config.get("multi_badge_size_ratio"),
        DEFAULT_CONFIG["multi_badge_size_ratio"],
    )

    config["notification_enabled"] = _coerce_bool(
        config.get("notification_enabled"),
        DEFAULT_CONFIG["notification_enabled"],
    )
    config["tg_token"] = str(config.get("tg_token") or "").strip()
    config["tg_chat_id"] = str(config.get("tg_chat_id") or "").strip()
    config["tmdb_api_key"] = str(config.get("tmdb_api_key") or "").strip()
    config["aggregate_enabled"] = _coerce_bool(config.get("aggregate_enabled"), DEFAULT_CONFIG["aggregate_enabled"])
    config["aggregate_time"] = _coerce_number(config.get("aggregate_time"), DEFAULT_CONFIG["aggregate_time"], as_int=True)
    config["scheduler_enabled"] = _coerce_bool(config.get("scheduler_enabled"), DEFAULT_CONFIG["scheduler_enabled"])
    config["scheduler_cron"] = str(config.get("scheduler_cron") or DEFAULT_CONFIG["scheduler_cron"]).strip()

    return config


def load_config() -> Dict[str, Any]:
    config = DEFAULT_CONFIG.copy()
    try:
        if CONFIG_FILE.exists():
            with open(CONFIG_FILE, encoding="utf-8") as f:
                user_config = json.load(f)
            config = normalize_config(user_config)
            logger.info(f"配置已从 {CONFIG_FILE} 加载")
    except Exception as e:
        logger.error(f"加载配置失败: {e}")
        config = DEFAULT_CONFIG.copy()
    return config


def save_config(config: Dict[str, Any]) -> bool:
    try:
        CONFIG_FILE.parent.mkdir(parents=True, exist_ok=True)
        normalized = normalize_config(config)
        with open(CONFIG_FILE, "w", encoding="utf-8") as f:
            json.dump(normalized, f, indent=2, ensure_ascii=False)
        logger.info(f"配置已保存到 {CONFIG_FILE}")
        return True
    except Exception as e:
        logger.error(f"保存配置失败: {e}")
        return False
