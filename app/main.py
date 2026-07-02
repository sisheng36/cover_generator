import logging
import sys
from pathlib import Path
from contextlib import asynccontextmanager

from fastapi import FastAPI, Request, UploadFile, File, Form
from fastapi.responses import HTMLResponse, JSONResponse, FileResponse
from fastapi.staticfiles import StaticFiles

from .styles.style_single_1 import create_style_single_1
from .styles.style_single_2 import create_style_single_2
from .styles.style_multi_1 import create_style_multi_1
from .config_manager import load_config, save_config

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(name)s: %(message)s")
logger = logging.getLogger("EmbyTool")

FONTS_DIR = Path(__file__).parent.parent / "fonts"
UPLOAD_DIR = Path("/data/uploads")
UPLOAD_DIR.mkdir(parents=True, exist_ok=True)
OUTPUT_DIR = Path("/data/covers_output")
OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

app_config: dict = {}


@asynccontextmanager
async def lifespan(app: FastAPI):
    global app_config
    app_config = load_config()
    logger.info("EmbyTool 已启动")
    yield
    logger.info("EmbyTool 已停止")


app = FastAPI(title="EmbyTool", version="2.0.0", lifespan=lifespan)
app.mount("/static", StaticFiles(directory=Path(__file__).parent / "static"), name="static")


@app.get("/", response_class=HTMLResponse)
async def index():
    html = (Path(__file__).parent / "static" / "index.html").read_text(encoding="utf-8")
    return HTMLResponse(html)


@app.get("/api/config")
async def get_config():
    return JSONResponse(app_config)


@app.post("/api/config")
async def update_config(request: Request):
    global app_config
    body = await request.json()
    app_config.update(body)
    save_config(app_config)
    return JSONResponse({"ok": True, "message": "配置已保存"})


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

    config = {
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
                item_count=item_count, config=config,
            )
        elif cover_style == "single_2":
            result = create_style_single_2(
                str(input_path), (title_zh, title_en), (zh_font, en_font),
                font_size=(zh_font_size, en_font_size),
                blur_size=blur_size, color_ratio=color_ratio,
                item_count=item_count, config=config,
            )
        elif cover_style == "multi_1":
            lib_dir = UPLOAD_DIR / "multi_temp"
            lib_dir.mkdir(parents=True, exist_ok=True)
            for i in range(1, 10):
                target = lib_dir / f"{i}.jpg"
                if not target.exists():
                    import shutil
                    shutil.copy(input_path, target)
            result = create_style_multi_1(
                str(lib_dir), (title_zh, title_en), (zh_font_multi, en_font_multi),
                font_size=(zh_font_size, en_font_size),
                is_blur=False, blur_size=blur_size, color_ratio=color_ratio,
                item_count=item_count, config=config,
            )
        else:
            return JSONResponse({"ok": False, "message": f"未知风格: {cover_style}"}, status_code=400)

        if result is False:
            return JSONResponse({"ok": False, "message": "封面生成失败"}, status_code=500)

        import base64
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


@app.get("/api/health")
async def health():
    return JSONResponse({"status": "ok"})


if __name__ == "__main__":
    import uvicorn
    uvicorn.run("app.main:app", host="0.0.0.0", port=8055)
