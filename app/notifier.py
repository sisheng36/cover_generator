import logging
import re
import time
import threading
import json
from typing import Any, Dict, List, Optional, Tuple
from urllib.parse import urlparse

import requests

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


def extract_tmdb_id(item: dict) -> Optional[str]:
    path = item.get("Path") or ""
    for pattern in TMDB_ID_PATTERNS:
        m = pattern.search(path)
        if m:
            return m.group(1)
    provider_ids = item.get("ProviderIds") or {}
    return provider_ids.get("Tmdb")


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
        "series_id": str(series_id) if series_id else None,
        "season_id": int(season_id) if season_id is not None else None,
        "episode_id": int(episode_id) if episode_id is not None else None,
        "tmdb_id": tmdb_id,
        "overview": item.get("Overview", ""),
        "image_url": None,
        "item_path": item.get("Path", ""),
        "json_object": data,
    }


def fetch_tmdb_info(tmdb_api_key: str, tmdb_id: str, season: int = None) -> dict:
    if not tmdb_id or not tmdb_api_key:
        return {}
    base = "https://api.themoviedb.org/3"
    try:
        if season is not None:
            url = f"{base}/tv/{tmdb_id}/season/{season}"
        else:
            url = f"{base}/tv/{tmdb_id}"
        resp = requests.get(url, params={"api_key": tmdb_api_key, "language": "zh-CN"}, timeout=10)
        if resp.status_code == 200:
            return resp.json()
        url_en = f"{base}/tv/{tmdb_id}"
        resp_en = requests.get(url_en, params={"api_key": tmdb_api_key, "language": "en-US"}, timeout=10)
        if resp_en.status_code == 200:
            return resp_en.json()
    except Exception as e:
        logger.warning(f"TMDB API 请求失败: {e}")
    return {}


def merge_continuous_episodes(events: List[dict], tmdb_info: dict = None) -> str:
    season_episodes: Dict[int, list] = {}
    for ev in events:
        s = ev.get("season_id")
        e = ev.get("episode_id")
        if s is not None and e is not None:
            season_episodes.setdefault(int(s), []).append({"episode": int(e), "name": ev.get("item_name", "")})

    merged = []
    for season in sorted(season_episodes.keys()):
        eps = sorted(season_episodes[season], key=lambda x: x["episode"])
        if not eps:
            continue
        start = end = eps[0]["episode"]
        names = [eps[0]["name"]]
        for i in range(1, len(eps)):
            if eps[i]["episode"] == end + 1:
                end = eps[i]["episode"]
                names.append(eps[i]["name"])
            else:
                if start == end:
                    merged.append(f"S{season:02d}E{start:02d} {names[0]}" if names[0] else f"S{season:02d}E{start:02d}")
                else:
                    merged.append(f"S{season:02d}E{start:02d}-E{end:02d}")
                start = end = eps[i]["episode"]
                names = [eps[i]["name"]]
        if start == end:
            final_name = names[-1] if names else ""
            merged.append(f"S{season:02d}E{start:02d} {final_name}" if final_name else f"S{season:02d}E{start:02d}")
        else:
            merged.append(f"S{season:02d}E{start:02d}-E{end:02d}")
    return ", ".join(merged) if merged else ""


def build_tv_message(events: List[dict], tmdb_api_key: str) -> Tuple[str, str, Optional[str]]:
    first = events[0]
    show_name = first.get("item_name", "")
    for ev in events:
        jo = ev.get("json_object") or {}
        sn = (jo.get("Item") or {}).get("SeriesName")
        if sn:
            show_name = sn
            break

    is_multi = len(events) > 1
    tmdb_id = first.get("tmdb_id")
    tmdb_info = fetch_tmdb_info(tmdb_api_key, tmdb_id, first.get("season_id")) if tmdb_id else {}

    title = f"📺 新入库剧集：{show_name}"
    if is_multi:
        title += f" {len(events)}个文件"

    texts = [f"⏰ 时间：{time.strftime('%Y-%m-%d %H:%M:%S')}"]
    episodes_detail = merge_continuous_episodes(events, tmdb_info)
    if episodes_detail:
        texts.append(f"📺 季集：{episodes_detail}")

    if tmdb_info:
        vote = tmdb_info.get("vote_average")
        if vote:
            texts.append(f"⭐ 评分：{round(float(vote), 1)}/10")
        genres = tmdb_info.get("genres", [])
        if genres:
            glist = [g["name"] for g in genres[:3] if isinstance(g, dict) and g.get("name")]
            if glist:
                texts.append(f"🎭 类型：{'、'.join(glist)}")

    overview = first.get("overview", "")
    if not overview and tmdb_info:
        overview = tmdb_info.get("overview", "")
    if tmdb_info and not is_multi and first.get("episode_id") is not None:
        eps_list = tmdb_info.get("episodes", [])
        ep_idx = int(first["episode_id"]) - 1
        if 0 <= ep_idx < len(eps_list):
            overview = eps_list[ep_idx].get("overview", overview)
    if overview:
        overview = overview[:100] + "..." if len(overview) > 100 else overview
        texts.append(f"📖 剧情：{overview}")

    image_url = None
    if tmdb_info:
        if is_multi:
            bp = tmdb_info.get("backdrop_path") or tmdb_info.get("poster_path")
        else:
            bp = tmdb_info.get("poster_path") or tmdb_info.get("backdrop_path")
        if bp:
            image_url = f"https://image.tmdb.org/t/p/original{bp}"

    return title, "\n".join(texts), image_url


def build_generic_message(event_info: dict) -> Tuple[str, str, Optional[str]]:
    event_action = event_info.get("event", "")
    action_text = WEBHOOK_ACTIONS.get(event_action, event_action)
    item_type = event_info.get("item_type", "")
    item_name = event_info.get("item_name", "")

    type_map = {"MOV": "电影", "TV": "剧集", "AUD": "有声书"}
    type_label = type_map.get(item_type, "")
    title = f"{action_text}{type_label} {item_name}" if type_label and item_name else f"{action_text}"

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
    if overview:
        texts.append(f"剧情：{overview}")
    texts.append(f"时间：{time.strftime('%Y-%m-%d %H:%M:%S')}")

    image_url = event_info.get("image_url")
    return title, "\n".join(texts), image_url


def send_telegram(token: str, chat_id: str, title: str, text: str, image_url: str = None) -> bool:
    if not token or not chat_id:
        logger.warning("Telegram 未配置")
        return False
    try:
        if image_url:
            caption = f"*{title}*\n{text}"[:1024]
            resp = requests.post(
                f"https://api.telegram.org/bot{token}/sendPhoto",
                data={"chat_id": chat_id, "photo": image_url, "caption": caption, "parse_mode": "Markdown"},
                timeout=15,
            )
            if resp.status_code == 200:
                return True
            logger.warning(f"Telegram sendPhoto 失败 ({resp.status_code}), 降级为 sendMessage")

        payload = f"*{title}*\n{text}"
        resp = requests.post(
            f"https://api.telegram.org/bot{token}/sendMessage",
            data={"chat_id": chat_id, "text": payload, "parse_mode": "Markdown"},
            timeout=15,
        )
        return resp.status_code == 200
    except Exception as e:
        logger.error(f"Telegram 发送失败: {e}")
        return False


def send_aggregated_message(series_id: str, tmdb_api_key: str, tg_token: str, tg_chat_id: str):
    events = _pending_messages.pop(series_id, [])
    _aggregate_timers.pop(series_id, None)
    if not events:
        return
    try:
        title, text, image_url = build_tv_message(events, tmdb_api_key)
        logger.info(f"发送聚合消息: {title}")
        send_telegram(tg_token, tg_chat_id, title, text, image_url)
    except Exception as e:
        logger.error(f"发送聚合消息失败: {e}", exc_info=True)


def handle_webhook(data: dict, config: dict) -> str:
    enabled = config.get("notification_enabled", False)
    tg_token = config.get("tg_token", "")
    tg_chat_id = config.get("tg_chat_id", "")
    tmdb_api_key = config.get("tmdb_api_key", "")
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
            timer = threading.Timer(aggregate_time, send_aggregated_message, [series_id, tmdb_api_key, tg_token, tg_chat_id])
            _aggregate_timers[series_id] = timer
            timer.start()
            return f"aggregated, queue size: {len(_pending_messages[series_id])}"

    title, text, image_url = build_generic_message(event_info)
    send_telegram(tg_token, tg_chat_id, title, text, image_url)
    return "ok"
