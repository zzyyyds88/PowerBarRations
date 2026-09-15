#!/usr/bin/env python3
"""生成 PBR 品牌资产（logo.png / favicon.ico）。设计：深色圆角方 + 青色电源符号 + PBR 字标。"""
from PIL import Image, ImageDraw, ImageFont
import os

OUT = os.path.join(os.path.dirname(__file__), "..", "public")
OUT = os.path.abspath(OUT)
BG = (11, 18, 32, 255)
ACCENT = (56, 189, 248, 255)
ACCENT2 = (129, 230, 217, 255)
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"


def rounded(size, radius):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=radius, fill=BG)
    return img


def power_glyph(size):
    """电源符号：圆环 + 竖线。"""
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    w = max(2, size // 12)
    pad = size * 0.18
    d.arc([pad, pad, size - pad, size - pad], start=300, end=240, fill=ACCENT, width=w)
    cx = size / 2
    d.line([cx, size * 0.16, cx, size * 0.5], fill=ACCENT2, width=w)
    return img


def make_logo(size):
    img = rounded(size, int(size * 0.22))
    g = power_glyph(int(size * 0.56))
    img.alpha_composite(g, (int(size * 0.22), int(size * 0.22)))
    return img


def make_wordmark(w=512, h=512):
    img = rounded(w, int(w * 0.22))
    d = ImageDraw.Draw(img)
    font = ImageFont.truetype(FONT, int(h * 0.30))
    text = "PBR"
    box = d.textbbox((0, 0), text, font=font)
    tw, th = box[2] - box[0], box[3] - box[1]
    d.text(((w - tw) / 2 - box[0], (h - th) / 2 - box[1] + h * 0.16), text, font=font, fill=ACCENT)
    g = power_glyph(int(w * 0.34))
    img.alpha_composite(g, (int(w * 0.33), int(h * 0.14)))
    return img


if __name__ == "__main__":
    os.makedirs(OUT, exist_ok=True)
    logo = make_wordmark(512, 512)
    logo.save(os.path.join(OUT, "logo.png"))
    icon = make_logo(256)
    icon.save(os.path.join(OUT, "favicon.ico"), sizes=[(16, 16), (32, 32), (48, 48), (64, 64)])
    print("wrote", os.path.join(OUT, "logo.png"), os.path.join(OUT, "favicon.ico"))
