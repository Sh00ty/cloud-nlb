#!/usr/bin/env python3
"""Превращает тёмные скриншоты (тёмная тема Grafana и т.п.) в светлые,
пригодные для печати: фон становится белым, а цвета линий сохраняются.

В отличие от обычного негатива (инверсии RGB), который перекрашивает оттенки
(зелёный становится малиновым, синий оранжевым), здесь инвертируется только
яркость в пространстве HSV. Дополнительно гасится насыщенность в светлых
областях, чтобы фон вышел действительно белым, а не цветным — так печать
расходует меньше краски.

Использование:
  # рядом появится <имя>_light.png, оригинал не трогается
  python3 nir_text/darkfig_to_light.py nir_text/nlb_nir/many_experiments.png

  # заменить на месте, сохранив оригинал как <имя>_dark.png
  python3 nir_text/darkfig_to_light.py --inplace nir_text/nlb_nir/*.png

  # сильнее/слабее выбеливать фон (0..1, по умолчанию 0.85)
  python3 nir_text/darkfig_to_light.py --alpha 0.9 fig.png
"""
import argparse
import sys
from pathlib import Path

from PIL import Image, ImageOps, ImageChops


def to_light(src: Image.Image, alpha: float = 0.85) -> Image.Image:
    """Инверсия яркости с сохранением оттенка и выбеливанием фона."""
    h, s, v = src.convert("RGB").convert("HSV").split()
    v_inv = ImageOps.invert(v)
    # Чем светлее результат (фон), тем сильнее гасим насыщенность.
    factor = v_inv.point(lambda x: max(0, 255 - int(alpha * x)))
    s_new = ImageChops.multiply(s, factor)
    return Image.merge("HSV", (h, s_new, v_inv)).convert("RGB")


def main() -> int:
    ap = argparse.ArgumentParser(description="Тёмные рисунки -> светлые для печати.")
    ap.add_argument("images", nargs="+", type=Path, help="PNG/JPG файлы.")
    ap.add_argument("--alpha", type=float, default=0.85,
                    help="Сила выбеливания фона, 0..1 (по умолчанию 0.85).")
    ap.add_argument("--inplace", action="store_true",
                    help="Заменить файл, сохранив оригинал как <имя>_dark.<ext>.")
    ap.add_argument("--suffix", default="_light",
                    help="Суффикс для новой версии без --inplace (по умолчанию _light).")
    args = ap.parse_args()

    for path in args.images:
        if not path.exists():
            print(f"! нет файла: {path}", file=sys.stderr)
            continue
        light = to_light(Image.open(path), args.alpha)
        if args.inplace:
            backup = path.with_name(f"{path.stem}_dark{path.suffix}")
            if not backup.exists():
                path.replace(backup)
            else:
                print(f"  бэкап уже есть, не перезаписываю: {backup}", file=sys.stderr)
            light.save(path)
            print(f"{path}  (оригинал -> {backup.name})")
        else:
            out = path.with_name(f"{path.stem}{args.suffix}{path.suffix}")
            light.save(out)
            print(out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
