import logging
import base64
import shutil
from pathlib import Path
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request, UploadFile, File, Form
from fastapi.responses import HTMLResponse, JSONResponse
from fastapi.staticfiles import StaticFiles

from .styles.style_single_1 import create_style_single_1
from .styles.style_single_2 import create_style_single_2
from .styles.style_multi_1 import create_style_multi_1
from .config_manager import load_config, save_config
from .notifier import handle_webhook
from .emby_client import EmbyClient
from .cover_service import generate_cover_for_library
from .scheduler import start as sched_start, stop as sched_stop, is_running as sched_running, get_next_run as sched_next_run

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s")
logger = logging.getLogger("EmbyTool")

FONTS_DIR = Path(__file__).parent.parent / "fonts"
UPLOAD_DIR = Path("/data/uploads")
UPLOAD_DIR.mkdir(parents=True, exist_ok=True)
OUTPUT_DIR = Path("/data/covers_output")
OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

app_config: dict = {}


def _new_client() -> EmbyClient:
    return EmbyClient(
        app_config.get("emby_server_url", ""),
        app_config.get("emby_api_key", ""),
    )


def _scheduled_generate():
    config = load_config()
    client = EmbyClient(
        config.get("emby_server_url", ""),
        config.get("emby_api_key", ""),
    )
    libs = client.get_libraries()
    selected = config.get("scheduled_libraries", [])
    if selected:
        libs = [lib for lib in libs if lib.get("Id") in selected]
    if not libs:
        logger.warning("定时任务：没有可用的媒体库")
        return
    for lib in libs:
        result = generate_cover_for_library(client, lib, config)
        logger.info(f"定时任务 [{lib['Name']}] {result['message']}")


@asynccontextmanager
async def lifespan(app: FastAPI):
    global app_config
    app_config = load_config()
    sched_start(app_config, _scheduled_generate)
    logger.info("EmbyTool 已启动")
    yield
    sched_stop()
    logger.info("EmbyTool 已停止")


app = FastAPI(title="EmbyTool", version="2.0.0", lifespan=lifespan)
app.mount("/static", StaticFiles(directory=Path(__file__).parent / "static"), name="static")


@app.get("/", response_class=HTMLResponse)
async def index():
    html = (Path(__file__).parent / "static" / "index.html").read_text(encoding="utf-8")
    return HTMLResponse(html)


# ── 配置 ──

@app.get("/api/config")
async def get_config():
    return JSONResponse(app_config)


@app.post("/api/config")
async def update_config(request: Request):
    global app_config
    body = await request.json()
    app_config.update(body)
    save_config(app_config)
    sched_start(app_config, _scheduled_generate)
    return JSONResponse({"ok": True, "message": "配置已保存"})


# ── Emby 媒体库管理 ──

@app.get("/api/libraries")
async def list_libraries():
    client = _new_client()
    libs = client.get_libraries()
    items = []
    for lib in libs:
        items.append({
            "id": lib.get("Id"),
            "name": lib.get("Name"),
            "type": lib.get("CollectionType") or lib.get("Type", ""),
        })
    return JSONResponse({"ok": True, "libraries": items})


@app.post("/api/libraries/generate")
async def generate_libraries(request: Request):
    body = await request.json()
    library_ids = body.get("library_ids", [])
    if not library_ids:
        return JSONResponse({"ok": False, "message": "请选择媒体库"}, status_code=400)

    client = _new_client()
    all_libs = client.get_libraries()
    selected = [lib for lib in all_libs if lib.get("Id") in library_ids]

    if not selected:
        return JSONResponse({"ok": False, "message": "未找到指定媒体库"}, status_code=404)

    results = []
    for lib in selected:
        result = generate_cover_for_library(client, lib, app_config)
        results.append({"library": lib["Name"], **result})
        logger.info(f"[{lib['Name']}] {result['message']}")

    return JSONResponse({"ok": True, "results": results})


@app.post("/api/libraries/generate_all")
async def generate_all():
    client = _new_client()
    libs = client.get_libraries()
    selected_libs = app_config.get("selected_libraries", [])

    if selected_libs:
        libs = [lib for lib in libs if lib.get("Id") in selected_libs]

    if not libs:
        return JSONResponse({"ok": False, "message": "没有可用的媒体库"}, status_code=400)

    results = []
    for lib in libs:
        result = generate_cover_for_library(client, lib, app_config)
        results.append({"library": lib["Name"], **result})
        logger.info(f"[{lib['Name']}] {result['message']}")

    return JSONResponse({"ok": True, "results": results})


# ── 手动封面生成（上传图片）──

@app.post("/api/generate")
async def generate_cover(
    image: UploadFile = File(...),
    title_zh: str = Form(""),
    title_en: str = Form(""),
    cover_style: str = Form("single_1"),
    blur_size: int = Form(50),
    color_ratio: float = Form(0.8),
    zh_font_size: float = Form(1.0),
    en_font_size: float = Form(1.0),
    show_item_count: bool = Form(False),
    badge_style: str = Form("badge"),
    badge_size_ratio: float = Form(0.12),
    item_count: int = Form(None),
):
    ext = Path(image.filename).suffix if image.filename else ".jpg"
    input_path = UPLOAD_DIR / f"input{ext}"
    with open(input_path, "wb") as f:
        f.write(await image.read())

    zh_font = str(FONTS_DIR / "zh_font.ttf")
    en_font = str(FONTS_DIR / "en_font.ttf")
    zh_font_multi = str(FONTS_DIR / "zh_font_multi_1.ttf")
    en_font_multi = str(FONTS_DIR / "en_font_multi_1.otf")

    cfg = {
        "show_item_count": show_item_count,
        "badge_style": badge_style,
        "badge_size_ratio": badge_size_ratio,
    }

    try:
        if cover_style == "single_1":
            result = create_style_single_1(
                str(input_path), (title_zh, title_en), (zh_font, en_font),
                font_size=(zh_font_size, en_font_size),
                blur_size=blur_size, color_ratio=color_ratio,
                item_count=item_count, config=cfg,
            )
        elif cover_style == "single_2":
            result = create_style_single_2(
                str(input_path), (title_zh, title_en), (zh_font, en_font),
                font_size=(zh_font_size, en_font_size),
                blur_size=blur_size, color_ratio=color_ratio,
                item_count=item_count, config=cfg,
            )
        elif cover_style == "multi_1":
            lib_dir = UPLOAD_DIR / "multi_temp"
            lib_dir.mkdir(parents=True, exist_ok=True)
            for i in range(1, 10):
                target = lib_dir / f"{i}.jpg"
                if not target.exists():
                    shutil.copy(input_path, target)
            result = create_style_multi_1(
                str(lib_dir), (title_zh, title_en), (zh_font_multi, en_font_multi),
                font_size=(zh_font_size, en_font_size),
                is_blur=False, blur_size=blur_size, color_ratio=color_ratio,
                item_count=item_count, config=cfg,
            )
        else:
            return JSONResponse({"ok": False, "message": f"未知风格: {cover_style}"}, status_code=400)

        if result is False:
            return JSONResponse({"ok": False, "message": "封面生成失败"}, status_code=500)

        output_path = OUTPUT_DIR / "cover.png"
        image_data = base64.b64decode(result)
        with open(output_path, "wb") as f:
            f.write(image_data)

        return JSONResponse({
            "ok": True,
            "image_base64": f"data:image/png;base64,{result}",
        })
    except Exception as e:
        logger.exception("封面生成失败")
        return JSONResponse({"ok": False, "message": str(e)}, status_code=500)


@app.get("/api/scheduler/status")
async def scheduler_status():
    return JSONResponse({
        "running": sched_running(),
        "next_run": sched_next_run(),
        "enabled": app_config.get("scheduler_enabled", False),
        "cron": app_config.get("scheduler_cron", ""),
    })


@app.post("/api/scheduler/restart")
async def scheduler_restart():
    global app_config
    sched_start(app_config, _scheduled_generate)
    return JSONResponse({
        "ok": True,
        "running": sched_running(),
        "next_run": sched_next_run(),
    })


@app.get("/api/health")
async def health():
    return JSONResponse({"status": "ok"})


@app.post("/webhook/emby")
async def webhook_emby(request: Request):
    try:
        data = await request.json()
    except Exception:
        return JSONResponse({"ok": False, "message": "invalid JSON"}, status_code=400)

    result = handle_webhook(data, app_config)
    logger.info(f"Webhook 处理结果: {result}")
    return JSONResponse({"ok": True, "message": result})


if __name__ == "__main__":
    import uvicorn
    uvicorn.run("app.main:app", host="0.0.0.0", port=8055)
