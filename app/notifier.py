import logging
import re
import time
import threading
import html
from typing import Any, Dict, List, Optional, Tuple

import requests
from .emby_client import EmbyClient

logger = logging.getLogger(__name__)

TMDB_ID_PATTERNS = [
    re.compile(r'[\[{](?:tmdbid|tmdb)[=:-](\d+)[\]}]', re.I),
    re.compile(r'tmdb[=:-](\d+)', re.I),
    re.compile(r'tmdbid[=:-](\d+)', re.I),
]

AGGREGATE_TIME = 15
_pending_messages: Dict[str, list] = {}
_aggregate_timers: Dict[str, threading.Timer] = {}
_dedupe_cache: Dict[str, float] = {}
DEDUPE_EXPIRATION = 30

WEBHOOK_ACTIONS = {
    "library.new": "新入库",
    "ItemAdded": "新入库",
    "system.notificationtest": "测试",
    "playback.start": "开始播放",
    "playback.stop": "停止播放",
    "user.authenticated": "登录成功",
    "user.authenticationfailed": "登录失败",
    "media.play": "开始播放",
    "media.stop": "停止播放",
    "PlaybackStart": "开始播放",
    "PlaybackStop": "停止播放",
    "item.rate": "标记了",
}

MEDIA_ICONS = {
    "MOV": "🎬",
    "TV": "📺",
    "AUD": "🎧",
    "BOX": "📦",
}


def extract_tmdb_id(item: dict) -> Optional[str]:
    path = item.get("Path") or ""
    for pattern in TMDB_ID_PATTERNS:
        m = pattern.search(path)
        if m:
            return m.group(1)
    provider_ids = item.get("ProviderIds") or {}
    return provider_ids.get("Tmdb")


def _extract_year_from_value(value: Any) -> Optional[str]:
    if value in (None, ""):
        return None
    if isinstance(value, int):
        return str(value)
    text = str(value).strip()
    match = re.search(r"\b(\d{4})\b", text)
    return match.group(1) if match else None


def _event_item(event_info: dict) -> dict:
    return (event_info.get("json_object") or {}).get("Item") or {}


def _resolve_display_name(event_info: dict) -> str:
    item = _event_item(event_info)
    if event_info.get("item_type") == "TV":
        return (
            event_info.get("series_name")
            or item.get("SeriesName")
            or event_info.get("item_name")
            or item.get("Name")
            or ""
        )
    return event_info.get("item_name") or item.get("Name") or ""


def _resolve_year(event_info: dict, tmdb_info: Optional[dict] = None) -> Optional[str]:
    item = _event_item(event_info)
    candidates = [
        event_info.get("production_year"),
        item.get("ProductionYear"),
    ]
    if tmdb_info:
        candidates.extend([
            tmdb_info.get("first_air_date"),
            tmdb_info.get("release_date"),
        ])
    for value in candidates:
        year = _extract_year_from_value(value)
        if year:
            return year
    return None


def _build_library_title(name: str, item_type: str, year: Optional[str] = None) -> str:
    icon = MEDIA_ICONS.get(item_type, "🎬")
    title = name or "未知条目"
    if year:
        title = f"{title} ({year})"
    return f"{icon} {title} ✨ 入库成功"


def _truncate_text(text: str, limit: int) -> str:
    if len(text) <= limit:
        return text
    if limit <= 3:
        return text[:limit]
    return text[: limit - 3].rstrip() + "..."


def _format_telegram_payload(title: str, text: str, limit: Optional[int] = None) -> str:
    safe_title = f"<b>{html.escape(title)}</b>"
    if not text:
        return safe_title if limit is None else _truncate_text(safe_title, limit)

    raw_text = text.strip()
    if limit is not None:
        allowed = max(limit - len(safe_title) - 2, 0)
        raw_text = _truncate_text(raw_text, allowed)

    safe_text = html.escape(raw_text)
    return f"{safe_title}\n\n{safe_text}" if safe_text else safe_title


def _resolve_tmdb_image(tmdb_info: dict, item_type: str, prefer_backdrop: bool = False) -> Optional[str]:
    if not tmdb_info:
        return None
    if item_type == "TV" and prefer_backdrop:
        path = tmdb_info.get("backdrop_path") or tmdb_info.get("poster_path")
    else:
        path = tmdb_info.get("poster_path") or tmdb_info.get("backdrop_path")
    return f"https://image.tmdb.org/t/p/original{path}" if path else None


def _append_time_if_needed(lines: List[str]) -> List[str]:
    if not any(line.startswith("⏰ 时间:") for line in lines):
        lines.insert(1 if lines and lines[0].startswith("🎞️ ") else 0, f"⏰ 时间: {time.strftime('%Y-%m-%d %H:%M:%S')}")
    return lines


def _resolve_episode_line(events: List[dict]) -> str:
    episodes_detail = merge_continuous_episodes(events)
    if episodes_detail:
        return f"🎞️ 集数: {episodes_detail}"
    if len(events) > 1:
        return f"🎞️ 集数: 共{len(events)}个文件"
    first = events[0] if events else {}
    season_id = first.get("season_id")
    episode_id = first.get("episode_id")
    if season_id is not None and episode_id is not None:
        return f"🎞️ 集数: S{int(season_id):02d}E{int(episode_id):02d}"
    return "🎞️ 集数: 未知"


def _resolve_overview_line(overview: str) -> str:
    text = _truncate_text((overview or "").strip(), 240)
    return f"📝 剧情: {text or '暂无剧情'}"


def _download_emby_image(event_info: dict, server_url: str, api_key: str) -> Optional[Tuple[bytes, str]]:
    if not server_url or not api_key:
        return None
    item = _event_item(event_info)
    if not item:
        return None
    try:
        client = EmbyClient(server_url, api_key)
        api_path = client.get_image_url(item, use_primary=event_info.get("item_type") == "TV")
        if not api_path:
            fresh_item = client.get_item(event_info.get("item_id", ""))
            if fresh_item:
                api_path = client.get_image_url(fresh_item, use_primary=event_info.get("item_type") == "TV")
        if not api_path:
            return None
        resp = requests.get(f"{server_url.rstrip('/')}{api_path}", headers={"X-Emby-Token": api_key}, timeout=30)
        if resp.status_code != 200 or not resp.content:
            logger.warning(f"Emby 图片下载失败 {api_path} -> {resp.status_code}")
            return None
        return resp.content, "poster.jpg"
    except Exception as e:
        logger.warning(f"Emby 图片下载异常: {e}")
        return None


def parse_emby_webhook(data: dict) -> Optional[dict]:
    event = data.get("Event") or data.get("event") or ""
    item = data.get("Item") or {}
    server = data.get("Server") or {}

    if not event:
        return None

    event_action = event
    if event in ["ItemAdded"]:
        event_action = "library.new"

    item_type_raw = (item.get("Type") or "").upper()

    media_type_map = {
        "MOVIE": "MOV", "MOV": "MOV",
        "EPISODE": "TV", "TV": "TV", "SHOW": "TV",
        "MUSIC": "AUD", "AUDIO": "AUD", "AUDIOBOOK": "AUD",
        "BOXSET": "BOX",
    }
    item_type = media_type_map.get(item_type_raw, "MOV")
    if item_type == "TV":
        item_type = "TV" if item.get("ParentIndexNumber") is not None else "TV"
    elif item.get("SeriesId"):
        item_type = "TV"

    series_id = item.get("SeriesId") or item.get("SeriesName") or ""
    season_id = item.get("ParentIndexNumber")
    episode_id = item.get("IndexNumber")
    tmdb_id = extract_tmdb_id(item)

    return {
        "event": event_action,
        "server_name": server.get("Name", ""),
        "channel": "emby",
        "item_id": str(item.get("Id", "")),
        "item_type": item_type,
        "item_name": item.get("Name") or item.get("SeriesName", ""),
        "series_name": item.get("SeriesName", ""),
        "series_id": str(series_id) if series_id else None,
        "season_id": int(season_id) if season_id is not None else None,
        "episode_id": int(episode_id) if episode_id is not None else None,
        "tmdb_id": tmdb_id,
        "production_year": item.get("ProductionYear"),
        "overview": item.get("Overview", ""),
        "image_url": None,
        "item_path": item.get("Path", ""),
        "json_object": data,
    }


def fetch_tmdb_info(tmdb_api_key: str, tmdb_id: str, media_type: str = "tv") -> dict:
    if not tmdb_id or not tmdb_api_key:
        return {}
    media = "movie" if media_type == "movie" else "tv"
    base = f"https://api.themoviedb.org/3/{media}/{tmdb_id}"
    try:
        resp = requests.get(base, params={"api_key": tmdb_api_key, "language": "zh-CN"}, timeout=10)
        if resp.status_code == 200:
            return resp.json()
        resp_en = requests.get(base, params={"api_key": tmdb_api_key, "language": "en-US"}, timeout=10)
        if resp_en.status_code == 200:
            return resp_en.json()
    except Exception as e:
        logger.warning(f"TMDB API 请求失败: {e}")
    return {}


def fetch_tmdb_season_info(tmdb_api_key: str, tmdb_id: str, season: Optional[int]) -> dict:
    if not tmdb_id or not tmdb_api_key or season is None:
        return {}
    url = f"https://api.themoviedb.org/3/tv/{tmdb_id}/season/{season}"
    try:
        resp = requests.get(url, params={"api_key": tmdb_api_key, "language": "zh-CN"}, timeout=10)
        if resp.status_code == 200:
            return resp.json()
        resp_en = requests.get(url, params={"api_key": tmdb_api_key, "language": "en-US"}, timeout=10)
        if resp_en.status_code == 200:
            return resp_en.json()
    except Exception as e:
        logger.warning(f"TMDB 季信息请求失败: {e}")
    return {}


def merge_continuous_episodes(events: List[dict], tmdb_info: dict = None) -> str:
    season_episodes: Dict[int, list] = {}
    for ev in events:
        s = ev.get("season_id")
        e = ev.get("episode_id")
        if s is not None and e is not None:
            season_episodes.setdefault(int(s), []).append(int(e))

    merged = []
    for season in sorted(season_episodes.keys()):
        eps = sorted(set(season_episodes[season]))
        if not eps:
            continue
        start = end = eps[0]
        for i in range(1, len(eps)):
            if eps[i] == end + 1:
                end = eps[i]
            else:
                if start == end:
                    merged.append(f"S{season:02d}E{start:02d}")
                else:
                    merged.append(f"S{season:02d}E{start:02d}-S{season:02d}E{end:02d}")
                start = end = eps[i]
        if start == end:
            merged.append(f"S{season:02d}E{start:02d}")
        else:
            merged.append(f"S{season:02d}E{start:02d}-S{season:02d}E{end:02d}")
    return " ".join(merged) if merged else ""


def build_tv_message(events: List[dict], tmdb_api_key: str) -> Tuple[str, str, Optional[str]]:
    first = events[0]
    show_name = _resolve_display_name(first)
    for ev in events:
        jo = ev.get("json_object") or {}
        sn = (jo.get("Item") or {}).get("SeriesName")
        if sn:
            show_name = sn
            break

    is_multi = len(events) > 1
    tmdb_id = first.get("tmdb_id")
    tmdb_info = fetch_tmdb_info(tmdb_api_key, tmdb_id, "tv") if tmdb_id else {}
    season_info = fetch_tmdb_season_info(tmdb_api_key, tmdb_id, first.get("season_id")) if tmdb_id else {}

    title = _build_library_title(show_name, "TV", _resolve_year(first, tmdb_info))

    texts = [_resolve_episode_line(events)]

    overview = ""
    if is_multi and tmdb_info:
        overview = tmdb_info.get("overview", "")
    if not overview:
        overview = first.get("overview", "")
    if not overview and tmdb_info:
        overview = tmdb_info.get("overview", "")
    if season_info and not is_multi and first.get("episode_id") is not None:
        eps_list = season_info.get("episodes", [])
        ep_idx = int(first["episode_id"]) - 1
        if 0 <= ep_idx < len(eps_list):
            overview = eps_list[ep_idx].get("overview", overview)
    texts.append(_resolve_overview_line(overview))

    texts = _append_time_if_needed(texts)

    image_url = _resolve_tmdb_image(tmdb_info, "TV", prefer_backdrop=is_multi)

    return title, "\n".join(texts), image_url


def build_generic_message(event_info: dict, tmdb_api_key: str = "") -> Tuple[str, str, Optional[str]]:
    event_action = event_info.get("event", "")
    action_text = WEBHOOK_ACTIONS.get(event_action, event_action)
    item_type = event_info.get("item_type", "")
    display_name = _resolve_display_name(event_info)

    tmdb_info = {}
    if event_action == "library.new" and event_info.get("tmdb_id") and item_type in {"MOV", "TV"}:
        tmdb_info = fetch_tmdb_info(
            tmdb_api_key,
            event_info["tmdb_id"],
            "movie" if item_type == "MOV" else "tv",
        )

    if event_action == "library.new":
        title = _build_library_title(display_name, item_type, _resolve_year(event_info, tmdb_info))
    else:
        type_map = {"MOV": "电影", "TV": "剧集", "AUD": "有声书"}
        type_label = type_map.get(item_type, "")
        title = f"{action_text}{type_label} {display_name}" if type_label and display_name else f"{action_text}"

    texts = []
    user_name = event_info.get("user_name")
    if user_name:
        texts.append(f"用户：{user_name}")
    device = event_info.get("device_name") or event_info.get("client")
    if device:
        texts.append(f"设备：{device}")
    ip = event_info.get("ip")
    if ip:
        texts.append(f"IP地址：{ip}")
    percentage = event_info.get("percentage")
    if percentage:
        texts.append(f"进度：{round(float(percentage), 2)}%")
    overview = event_info.get("overview")
    if not overview and tmdb_info:
        overview = tmdb_info.get("overview")
    if item_type == "TV" and event_info.get("season_id") is not None and event_info.get("episode_id") is not None:
        texts.append(_resolve_episode_line([event_info]))
    if event_action == "library.new":
        texts.append(_resolve_overview_line(overview))
    elif overview:
        texts.append(f"📝 剧情: {_truncate_text(overview, 240)}")

    if event_action == "library.new" or texts:
        texts = _append_time_if_needed(texts)

    image_url = event_info.get("image_url") or _resolve_tmdb_image(tmdb_info, item_type, prefer_backdrop=item_type == "TV")
    return title, "\n".join(texts), image_url


def send_telegram(
    token: str,
    chat_id: str,
    title: str,
    text: str,
    image_url: str = None,
    image_bytes: bytes = None,
    image_name: str = "poster.jpg",
) -> bool:
    if not token or not chat_id:
        logger.warning("Telegram 未配置")
        return False
    try:
        caption = _format_telegram_payload(title, text, limit=1024)
        if image_bytes:
            resp = requests.post(
                f"https://api.telegram.org/bot{token}/sendPhoto",
                data={"chat_id": chat_id, "caption": caption, "parse_mode": "HTML"},
                files={"photo": (image_name, image_bytes)},
                timeout=30,
            )
            if resp.status_code == 200:
                return True
            logger.warning(f"Telegram sendPhoto(上传) 失败 ({resp.status_code}), 降级为 URL/文本")
        if image_url:
            resp = requests.post(
                f"https://api.telegram.org/bot{token}/sendPhoto",
                data={"chat_id": chat_id, "photo": image_url, "caption": caption, "parse_mode": "HTML"},
                timeout=15,
            )
            if resp.status_code == 200:
                return True
            logger.warning(f"Telegram sendPhoto 失败 ({resp.status_code}), 降级为 sendMessage")

        payload = _format_telegram_payload(title, text)
        resp = requests.post(
            f"https://api.telegram.org/bot{token}/sendMessage",
            data={"chat_id": chat_id, "text": payload, "parse_mode": "HTML"},
            timeout=15,
        )
        return resp.status_code == 200
    except Exception as e:
        logger.error(f"Telegram 发送失败: {e}")
        return False


def send_aggregated_message(
    series_id: str,
    tmdb_api_key: str,
    tg_token: str,
    tg_chat_id: str,
    emby_server_url: str,
    emby_api_key: str,
):
    events = _pending_messages.pop(series_id, [])
    _aggregate_timers.pop(series_id, None)
    if not events:
        return
    try:
        title, text, image_url = build_tv_message(events, tmdb_api_key)
        image_bytes = None
        image_name = "poster.jpg"
        if not image_url and events:
            downloaded = _download_emby_image(events[0], emby_server_url, emby_api_key)
            if downloaded:
                image_bytes, image_name = downloaded
        logger.info(f"发送聚合消息: {title}")
        send_telegram(tg_token, tg_chat_id, title, text, image_url, image_bytes, image_name)
    except Exception as e:
        logger.error(f"发送聚合消息失败: {e}", exc_info=True)


def handle_webhook(data: dict, config: dict) -> str:
    enabled = config.get("notification_enabled", False)
    tg_token = config.get("tg_token", "")
    tg_chat_id = config.get("tg_chat_id", "")
    tmdb_api_key = config.get("tmdb_api_key", "")
    emby_server_url = config.get("emby_server_url", "")
    emby_api_key = config.get("emby_api_key", "")
    notify_types = config.get("notify_types", [])
    aggregate_enabled = config.get("aggregate_enabled", True)
    aggregate_time = int(config.get("aggregate_time", 15))

    if not enabled:
        return "notifier disabled"

    event_info = parse_emby_webhook(data)
    if not event_info:
        return "unparseable webhook"

    event_action = event_info["event"]
    if event_action not in WEBHOOK_ACTIONS:
        return f"unsupported event: {event_action}"

    if notify_types:
        allowed = set()
        for t in notify_types:
            allowed.update(t.split("|"))
        if event_action not in allowed and event_action not in [WEBHOOK_ACTIONS.get(a) for a in allowed]:
            return f"event type not allowed: {event_action}"

    dedupe_key = f"{event_info['server_name']}-{event_action}-{event_info['item_id']}"
    now = time.time()
    if dedupe_key in _dedupe_cache and _dedupe_cache[dedupe_key] > now:
        return "duplicate"
    _dedupe_cache[dedupe_key] = now + DEDUPE_EXPIRATION

    if aggregate_enabled and event_action == "library.new" and event_info["item_type"] == "TV":
        series_id = event_info.get("series_id")
        if series_id:
            _pending_messages.setdefault(series_id, []).append(event_info)
            if series_id in _aggregate_timers:
                _aggregate_timers[series_id].cancel()
            timer = threading.Timer(
                aggregate_time,
                send_aggregated_message,
                [series_id, tmdb_api_key, tg_token, tg_chat_id, emby_server_url, emby_api_key],
            )
            _aggregate_timers[series_id] = timer
            timer.start()
            return f"aggregated, queue size: {len(_pending_messages[series_id])}"

    title, text, image_url = build_generic_message(event_info, tmdb_api_key)
    image_bytes = None
    image_name = "poster.jpg"
    if not image_url:
        downloaded = _download_emby_image(event_info, emby_server_url, emby_api_key)
        if downloaded:
            image_bytes, image_name = downloaded
    send_telegram(tg_token, tg_chat_id, title, text, image_url, image_bytes, image_name)
    return "ok"
