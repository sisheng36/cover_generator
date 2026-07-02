import json
import logging
from pathlib import Path
from typing import Dict, Any

logger = logging.getLogger(__name__)

CONFIG_FILE = Path("/data/config.json")

DEFAULT_CONFIG: Dict[str, Any] = {
    "emby_server_url": "",
    "emby_api_key": "",
    "cover_style": "single_1",
    "sort_by": "Random",
    "multi_1_blur": False,
    "multi_1_use_primary": True,
    "single_use_primary": False,
    "blur_size": 50,
    "color_ratio": 0.8,
    "zh_font_size": 1.0,
    "en_font_size": 1.0,
    "zh_font_size_multi_1": 1.0,
    "en_font_size_multi_1": 1.0,
    "blur_size_multi_1": 50,
    "color_ratio_multi_1": 0.8,
    "show_item_count": False,
    "badge_style": "badge",
    "badge_size_ratio": 0.12,
    "max_safe_rating": 8,
    "title_config": "",
    "covers_output": "/data/covers_output",
    "covers_input": "",
    "notification_enabled": False,
    "tg_token": "",
    "tg_chat_id": "",
    "tmdb_api_key": "",
    "notify_types": [],
    "aggregate_enabled": True,
    "aggregate_time": 15,
}


def load_config() -> Dict[str, Any]:
    config = DEFAULT_CONFIG.copy()
    try:
        if CONFIG_FILE.exists():
            with open(CONFIG_FILE) as f:
                user_config = json.load(f)
                config.update(user_config)
            logger.info(f"配置已从 {CONFIG_FILE} 加载")
    except Exception as e:
        logger.error(f"加载配置失败: {e}")
    return config


def save_config(config: Dict[str, Any]) -> bool:
    try:
        CONFIG_FILE.parent.mkdir(parents=True, exist_ok=True)
        with open(CONFIG_FILE, "w") as f:
            json.dump(config, f, indent=2, ensure_ascii=False)
        logger.info(f"配置已保存到 {CONFIG_FILE}")
        return True
    except Exception as e:
        logger.error(f"保存配置失败: {e}")
        return False
