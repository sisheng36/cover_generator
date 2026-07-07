"""
Generate EmbyTool favicon set — v3 (minimal).

Spec (user-driven, second iteration):
  1. Background = user-supplied 1:1 image (`电影 2.jpg`), with a heavy
     rounded-square mask. NO darkening veil, NO blur (we want the poster
     collage to be clearly visible).
  2. Liquid-glass border highlight: a thin white rim that's brighter on
     the top-left edge, fading toward bottom-right.
  3. Center = the Emby logo: a green 45°-rotated square outline with a
     green right-pointing play triangle inside. White parts of the logo
     are made fully transparent so the background poster shows through
     the negative space (the gap inside the diamond + the V-cut in the
     triangle).
  4. Top-right = a single solid red dot. No paper plane, no halo, no
     border, no shadow. Just a clean red circle.

Outputs (overwrites app/static/*):
  favicon.svg, favicon-32x32.png, favicon-16x16.png,
  apple-touch-icon.png (180), android-chrome-192x192.png,
  android-chrome-512x512.png, favicon.ico (16/32/48).
"""

from __future__ import annotations

import base64
from io import BytesIO
from pathlib import Path

import numpy as np
from PIL import Image, ImageDraw, ImageFilter, ImageFont

# --------------------------------------------------------------------------- #
# Paths                                                                       #
# --------------------------------------------------------------------------- #

ROOT = Path(__file__).resolve().parents[1]
OUT_DIR = ROOT / "app" / "static"
BG_PATH = Path("/Users/knight/Downloads/Safari/电影 2.jpg")
OUT_DIR.mkdir(parents=True, exist_ok=True)

# --------------------------------------------------------------------------- #
# Palette                                                                     #
# --------------------------------------------------------------------------- #

# Classic Emby "media play" green (matches the icon you sent)
EMBY_GREEN = (108, 192, 74)        # ~#6CC04A
EMBY_GREEN_DEEP = (82, 158, 56)    # ~#529E38, for subtle depth

# Solid notification red
NOTIFY_RED = (239, 68, 68)         # #ef4444


def _hex(rgb: tuple[int, int, int]) -> str:
    return "#{:02x}{:02x}{:02x}".format(*rgb)


# --------------------------------------------------------------------------- #
# Emby logo — vector geometry                                                 #
#                                                                             #
# The logo is a 45°-rotated square outline (a "diamond" frame) with a        #
# right-pointing play triangle in the middle. We compose it from:            #
#   • a thick green diamond (the frame)                                      #
#   • a smaller white square inside it (so the negative space is bg, NOT     #
#     white)                                                                 #
#   • a green play triangle centered inside the white square                 #
#                                                                             #
# All three pieces sit on the canvas with explicit transparency, so the       #
# poster background shows through the inner white area.                      #
# --------------------------------------------------------------------------- #

def _draw_emby_logo(base: Image.Image, size: int) -> None:
    """
    Compose the Emby "diamond + play" logo onto `base`.

    Geometry (in a 100×100 viewBox, then scaled):
      • Outer diamond: vertices at (50, 0), (100, 50), (50, 100), (0, 50)
        — a 45°-rotated square.
      • The diamond is a thick stroke: outer half-width 50, inner half-width
        ~36 → stroke width ~14.
      • Inner white square: 72×72 centered, rotated 45°, so it has the
        same diamond outline as the inside of the frame.
      • Play triangle: equilateral-ish, right-pointing, centered.

    White parts of the logo are realized by compositing a transparent
    white shape on top, so the background poster still shows through.
    """
    s = size
    cx, cy = s / 2, s / 2
    # Logo bounding box (square) inside the canvas
    box = int(s * 0.66)
    half = box / 2
    # Outer diamond half-diagonal = box/2
    r_outer = half
    r_inner = half * 0.72  # inner diamond radius (controls frame thickness)
    tri_w = box * 0.30    # triangle width

    # 1) Outer diamond frame (filled polygon = outer minus inner)
    diamond_outer = [
        (cx,           cy - r_outer),
        (cx + r_outer, cy          ),
        (cx,           cy + r_outer),
        (cx - r_outer, cy          ),
    ]
    diamond_inner = [
        (cx,           cy - r_inner),
        (cx + r_inner, cy          ),
        (cx,           cy + r_inner),
        (cx - r_inner, cy          ),
    ]
    frame = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    fd = ImageDraw.Draw(frame)
    fd.polygon(diamond_outer, fill=EMBY_GREEN + (255,))
    # Cut the inner hole using the destination's existing alpha by drawing
    # the inner polygon with alpha 0 — Pillow's polygon can't do even-odd
    # directly, so we composite a fresh layer with the inner shape as the
    # only filled region and use it as an erase mask.
    mask = Image.new("L", (s, s), 0)
    md = ImageDraw.Draw(mask)
    md.polygon(diamond_outer, fill=255)
    md.polygon(diamond_inner, fill=0)
    frame.putalpha(mask)

    base.alpha_composite(frame)

    # 2) Inner white square (rotated 45° → another diamond) — fully
    #    transparent so the poster shows through, but slightly tinted
    #    white to give the logo a soft "lifted" look without obscuring
    #    the bg. (The user asked for white to be transparent; we leave
    #    the inner area completely untouched — just the frame and the
    #    play triangle are drawn.)

    # 3) Play triangle, right-pointing, centered
    tri = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    td = ImageDraw.Draw(tri)
    tri_h = tri_w * (np.sqrt(3) / 2)
    # Centered at (cx, cy). Triangle tip on the right, base on the left.
    p1 = (cx - tri_w * 0.30, cy - tri_h * 0.55)
    p2 = (cx - tri_w * 0.30, cy + tri_h * 0.55)
    p3 = (cx + tri_w * 0.70, cy)
    td.polygon([p1, p2, p3], fill=EMBY_GREEN + (255,))
    base.alpha_composite(tri)


# --------------------------------------------------------------------------- #
# Red notification dot — solid, no decoration                                 #
# --------------------------------------------------------------------------- #

def _draw_notification_dot(base: Image.Image, size: int) -> None:
    """
    A single solid red dot in the top-right. No halo, no paper plane,
    no inner highlight, no border. Just a clean filled circle.
    """
    d = ImageDraw.Draw(base, "RGBA")
    cx = int(size * 0.82)
    cy = int(size * 0.18)
    r  = int(size * 0.075)
    d.ellipse((cx - r, cy - r, cx + r, cy + r), fill=NOTIFY_RED + (255,))


# --------------------------------------------------------------------------- #
# Liquid-glass border                                                         #
# --------------------------------------------------------------------------- #

def _draw_glass_border(base: Image.Image, size: int) -> None:
    """
    Liquid-glass rim highlight. Brighter on the top-left edge, fading to
    a subtle hairline on the bottom-right — like a wet-glass edge under
    a top-left light source.
    """
    # Top + left arc highlight (bright)
    rim = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    rd = ImageDraw.Draw(rim)
    rd.arc(
        (1, 1, size - 2, size - 2),
        start=180, end=270,                       # top-left quarter
        fill=(255, 255, 255, 220),
        width=max(2, size // 160),
    )
    rd.arc(
        (3, 3, size - 4, size - 4),
        start=180, end=270,
        fill=(255, 255, 255, 140),
        width=max(1, size // 220),
    )
    # Full hairline (very subtle, gives the shape a defined edge on
    # light backgrounds)
    rd.rounded_rectangle(
        (1, 1, size - 2, size - 2),
        radius=int(size * 0.22),
        outline=(255, 255, 255, 110),
        width=max(1, size // 256),
    )
    base.alpha_composite(rim)

    # Soft top gloss band — the "liquid" feel
    band_h = max(1, int(size * 0.10))
    arr = np.zeros((band_h, size, 4), dtype=np.uint8)
    arr[..., 0:3] = 255
    grad = np.linspace(40, 0, band_h, dtype=np.float32)
    arr[..., 3] = np.tile(grad[:, None], (1, size)).astype(np.uint8)
    gloss_layer = Image.fromarray(arr, mode="RGBA")
    # Clip the gloss to the rounded-square shape by masking it with the
    # corner mask before compositing.
    rounded = _rounded_mask(size, radius_ratio=0.22)
    # Combine the gloss alpha with the rounded-square alpha
    gloss_alpha = np.asarray(gloss_layer)[..., 3]
    rounded_arr = np.asarray(rounded)
    if gloss_alpha.shape != rounded_arr.shape:
        # Resize gloss alpha to match if band_h was clamped to 1
        gloss_layer = gloss_layer.resize((size, size), Image.BILINEAR)
        gloss_alpha = np.asarray(gloss_layer)[..., 3]
    combined = np.minimum(gloss_alpha, rounded_arr).astype(np.uint8)
    gloss_layer.putalpha(Image.fromarray(combined, mode="L"))
    base.alpha_composite(gloss_layer)


# --------------------------------------------------------------------------- #
# Helpers                                                                     #
# --------------------------------------------------------------------------- #

def _load_bg() -> Image.Image:
    img = Image.open(BG_PATH).convert("RGB")
    s = min(img.size)
    return img.crop(((img.size[0] - s) // 2, (img.size[1] - s) // 2,
                     (img.size[0] + s) // 2, (img.size[1] + s) // 2))


def _rounded_mask(size: int, radius_ratio: float = 0.22) -> Image.Image:
    mask = Image.new("L", (size, size), 0)
    d = ImageDraw.Draw(mask)
    d.rounded_rectangle((0, 0, size - 1, size - 1),
                        radius=int(size * radius_ratio), fill=255)
    return mask


# --------------------------------------------------------------------------- #
# SVG master                                                                  #
# --------------------------------------------------------------------------- #

def _build_svg(bg_b64: str, mime: str = "image/jpeg") -> str:
    return f"""<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 256 256" role="img" aria-label="EmbyTool">
  <defs>
    <clipPath id="iconClip">
      <rect x="4" y="4" width="248" height="248" rx="56"/>
    </clipPath>
    <linearGradient id="frameGreen" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0"   stop-color="{_hex(EMBY_GREEN)}"/>
      <stop offset="1"   stop-color="{_hex(EMBY_GREEN_DEEP)}"/>
    </linearGradient>
    <linearGradient id="rimGlow" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0"    stop-color="#ffffff" stop-opacity=".95"/>
      <stop offset="0.5"  stop-color="#ffffff" stop-opacity=".30"/>
      <stop offset="1"    stop-color="#ffffff" stop-opacity=".70"/>
    </linearGradient>
    <linearGradient id="topGloss" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#ffffff" stop-opacity=".35"/>
      <stop offset="1" stop-color="#ffffff" stop-opacity="0"/>
    </linearGradient>
  </defs>

  <!-- 1. Background: user image, masked to rounded square -->
  <g clip-path="url(#iconClip)">
    <image x="0" y="0" width="256" height="256" preserveAspectRatio="xMidYMid slice"
           xlink:href="data:{mime};base64,{bg_b64}"/>

    <!-- 2. Subtle top gloss band (the "liquid" sheen) -->
    <rect x="0" y="0" width="256" height="120" fill="url(#topGloss)"/>

    <!-- 3. Center Emby logo: green diamond frame + green play triangle.
            Drawn as a frame (outer diamond minus inner diamond) + a triangle.
            The inner area is left transparent so the poster shows through. -->
    <g transform="translate(128 128)">
      <!-- Frame: outer diamond -->
      <polygon points="0,-90 90,0 0,90 -90,0"
               fill="url(#frameGreen)"/>
      <!-- Frame: inner diamond (same fill as background — fully transparent,
           letting the underlying poster show through) -->
      <polygon points="0,-65 65,0 0,65 -65,0"
               fill="#ffffff" fill-opacity="0"/>
      <!-- Play triangle, right-pointing, centered -->
      <polygon points="-22,-32 -22,32 38,0"
               fill="url(#frameGreen)"/>
    </g>

    <!-- 4. Top-right red notification dot (solid, no decoration) -->
    <circle cx="210" cy="46" r="20" fill="{_hex(NOTIFY_RED)}"/>

    <!-- 5. Liquid-glass rim highlight -->
    <rect x="4.5" y="4.5" width="247" height="247" rx="55.5" fill="none"
          stroke="url(#rimGlow)" stroke-width="1.6"/>
  </g>
</svg>
"""


# --------------------------------------------------------------------------- #
# Renderer                                                                    #
# --------------------------------------------------------------------------- #

def _render(size: int, bg: Image.Image) -> Image.Image:
    # Background: just the poster, slightly softened (no darkening)
    bg = bg.resize((size, size), Image.LANCZOS)
    bg = bg.filter(ImageFilter.GaussianBlur(radius=max(1, size // 128)))
    canvas = bg.convert("RGBA")

    # Center logo
    _draw_emby_logo(canvas, size)

    # Red dot
    _draw_notification_dot(canvas, size)

    # Glass border (rim + top gloss)
    _draw_glass_border(canvas, size)

    # Clip to rounded square
    mask = _rounded_mask(size, radius_ratio=0.22)
    out = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    out.paste(canvas, (0, 0), mask)
    return out


# --------------------------------------------------------------------------- #
# Entrypoint                                                                  #
# --------------------------------------------------------------------------- #

def main() -> None:
    bg = _load_bg()

    # 1. SVG (with embedded base64 background)
    bg_for_svg = Image.open(BG_PATH).convert("RGB")
    bg_for_svg.thumbnail((512, 512), Image.LANCZOS)
    buf = BytesIO()
    bg_for_svg.save(buf, format="JPEG", quality=82, optimize=True)
    bg_b64 = base64.b64encode(buf.getvalue()).decode("ascii")
    (OUT_DIR / "favicon.svg").write_text(_build_svg(bg_b64), encoding="utf-8")
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
        img = _render(size, bg)
        img.save(OUT_DIR / name, format="PNG", optimize=True)
        print(f"wrote {name} ({size}x{size})")

    # 3. ICO (multi-size)
    ico_sizes = [16, 32, 48]
    base = _render(48, bg)
    base.save(
        OUT_DIR / "favicon.ico",
        format="ICO",
        sizes=[(s, s) for s in ico_sizes],
        append_images=[_render(s, bg) for s in ico_sizes[1:]],
    )
    print(f"wrote favicon.ico ({'/'.join(str(s) for s in ico_sizes)})")


if __name__ == "__main__":
    main()
