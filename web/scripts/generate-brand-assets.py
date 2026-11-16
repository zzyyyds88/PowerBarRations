#!/usr/bin/env python3
"""生成 PBR 前端品牌资产（logo.png / favicon.ico）。

源图：scripts/brand/icon-source.png
  由 tokenrhythm `qwen-image-2.0` 生成的 1024×1024 原始输出（白底 + 深色圆角方块
  + 霓虹青色闪电电源符号）。生成提示词：
    Minimalist flat vector app icon, rounded square, deep midnight navy blue
    background with subtle gradient, a single centered glowing electric-cyan
    power symbol: a vertical bar merged with a lightning bolt, soft neon glow,
    clean geometric shapes, high contrast, generous padding, crisp edges,
    no text, no letters, no words, professional tech logo, 1:1 square

本脚本把白底抠成透明、裁到图形外接框，再导出：
  public/logo.png     512×512 RGBA（控制台 logo 与浏览器 favicon）
  public/favicon.ico  16/32/48/64 多尺寸

用法：python3 scripts/generate-brand-assets.py
"""
from PIL import Image, ImageChops, ImageDraw
import os

HERE = os.path.dirname(os.path.abspath(__file__))
SOURCE = os.path.join(HERE, "brand", "icon-source.png")
OUT = os.path.abspath(os.path.join(HERE, "..", "public"))
SENTINEL = (255, 0, 255)
WHITE_THRESHOLD = 40
LOGO_SIZE = 512
FAVICON_SIZE = 256
FAVICON_SIZES = [(16, 16), (32, 32), (48, 48), (64, 64)]


def _seed_points(width, height):
    """白底与图形不连通时仍能覆盖的边界种子点。"""
    return [
        (0, 0),
        (width - 1, 0),
        (0, height - 1),
        (width - 1, height - 1),
        (width // 2, 0),
        (0, height // 2),
        (width - 1, height // 2),
        (width // 2, height - 1),
    ]


def cut_out_background(source):
    """把与图像边缘连通的白底抠成透明，并裁到图形外接框。"""
    rgb = source.convert("RGB")
    for point in _seed_points(*rgb.size):
        ImageDraw.floodfill(rgb, point, SENTINEL, thresh=WHITE_THRESHOLD)
    diff = ImageChops.difference(rgb, Image.new("RGB", rgb.size, SENTINEL))
    red, green, blue = diff.split()
    alpha = ImageChops.lighter(ImageChops.lighter(red, green), blue).point(
        lambda value: 0 if value == 0 else 255, "L"
    )
    rgba = source.convert("RGBA")
    rgba.putalpha(alpha)
    return rgba.crop(alpha.getbbox())


if __name__ == "__main__":
    art = cut_out_background(Image.open(SOURCE))
    os.makedirs(OUT, exist_ok=True)
    logo = art.resize((LOGO_SIZE, LOGO_SIZE), Image.LANCZOS)
    logo.save(os.path.join(OUT, "logo.png"), optimize=True)
    favicon = art.resize((FAVICON_SIZE, FAVICON_SIZE), Image.LANCZOS)
    favicon.save(os.path.join(OUT, "favicon.ico"), sizes=FAVICON_SIZES)
    print("wrote", os.path.join(OUT, "logo.png"), os.path.join(OUT, "favicon.ico"))
