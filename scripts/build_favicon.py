"""
Generate EmbyTool favicon set — v4 (single source).

Spec (user-driven, latest iteration):
  • Source = the already-composed icon design (`网站图标设计-3.png`):
    rounded-square glass tile with a movie-poster collage, the green Emby
    diamond + play icon centered, and a red notification dot in the
    top-right. This script does NOT re-compose any geometry — it only
    resamples / re-encodes the final artwork to every favicon size.
  • Outputs (overwrites app/static/*):
      favicon.svg                  — vector wrapper around the master PNG
      favicon-32x32.png            — 32x32 PNG
      favicon-16x16.png            — 16x16 PNG
      apple-touch-icon.png         — 180x180 PNG
      android-chrome-192x192.png   — 192x192 PNG
      android-chrome-512x512.png   — 512x512 PNG
      favicon.ico                  — multi-size ICO (16/32/48)
"""

from __future__ import annotations

import base64
from io import BytesIO
from pathlib import Path

from PIL import Image

# --------------------------------------------------------------------------- #
# Paths                                                                       #
# --------------------------------------------------------------------------- #

ROOT = Path(__file__).resolve().parents[1]
OUT_DIR = ROOT / "app" / "static"
SRC_PATH = Path("/Users/knight/Downloads/Safari/网站图标设计-3.png")
OUT_DIR.mkdir(parents=True, exist_ok=True)


# --------------------------------------------------------------------------- #
# SVG master                                                                  #
# --------------------------------------------------------------------------- #

def _build_svg(png_b64: str) -> str:
    """Wrap the source PNG in an SVG so browsers can scale it crisply."""
    return f"""<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 1024 1024" role="img" aria-label="EmbyTool">
  <image x="0" y="0" width="1024" height="1024"
         xlink:href="data:image/png;base64,{png_b64}"/>
</svg>
"""


def _png_to_b64(img: Image.Image) -> str:
    """Encode an RGBA PIL image as base64 PNG."""
    buf = BytesIO()
    img.save(buf, format="PNG", optimize=True)
    return base64.b64encode(buf.getvalue()).decode("ascii")


# --------------------------------------------------------------------------- #
# Renderer                                                                    #
# --------------------------------------------------------------------------- #

def _render(master: Image.Image, size: int) -> Image.Image:
    """Resample the master artwork to `size x size`, preserving alpha."""
    return master.resize((size, size), Image.LANCZOS)


# --------------------------------------------------------------------------- #
# Entrypoint                                                                  #
# --------------------------------------------------------------------------- #

def main() -> None:
    master = Image.open(SRC_PATH).convert("RGBA")
    print(f"loaded master: {master.size[0]}x{master.size[1]} from {SRC_PATH}")

    # 1. SVG — embed the master PNG as base64 so the icon scales cleanly.
    #    Use a 1024-wide copy to keep the embedded payload reasonable
    #    while staying well above every target size.
    svg_source = master.resize((1024, 1024), Image.LANCZOS)
    (OUT_DIR / "favicon.svg").write_text(
        _build_svg(_png_to_b64(svg_source)), encoding="utf-8"
    )
    print("wrote favicon.svg")

    # 2. PNG sizes
    targets = {
        "favicon-32x32.png":            32,
        "favicon-16x16.png":            16,
        "apple-touch-icon.png":        180,
        "android-chrome-192x192.png":  192,
        "android-chrome-512x512.png":  512,
    }
    for name, size in targets.items():
        img = _render(master, size)
        img.save(OUT_DIR / name, format="PNG", optimize=True)
        print(f"wrote {name} ({size}x{size})")

    # 3. ICO (multi-size)
    ico_sizes = [16, 32, 48]
    base = _render(master, ico_sizes[-1])
    base.save(
        OUT_DIR / "favicon.ico",
        format="ICO",
        sizes=[(s, s) for s in ico_sizes],
        append_images=[_render(master, s) for s in ico_sizes[:-1]],
    )
    print(f"wrote favicon.ico ({'/'.join(str(s) for s in ico_sizes)})")


if __name__ == "__main__":
    main()