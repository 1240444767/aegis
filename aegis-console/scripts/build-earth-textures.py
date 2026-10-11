"""生成概览页地球贴图（public/textures/earth/*-4k.*）。

源图全部来自 NASA Visible Earth（公有领域），先下载到同一目录：
  world.topo.bathy.200410.3x21600x10800.jpg  https://eoimages.gsfc.nasa.gov/images/imagerecords/73000/73826/
  world.200407.3x21600x10800.jpg             https://eoimages.gsfc.nasa.gov/images/imagerecords/74000/74092/
  BlackMarble_2016_3km.jpg                   https://eoimages.gsfc.nasa.gov/images/imagerecords/144000/144898/
  cloud_combined_8192.tif                    https://eoimages.gsfc.nasa.gov/images/imagerecords/57000/57747/
  gebco_08_rev_elev_21600x10800.png          https://eoimages.gsfc.nasa.gov/images/imagerecords/73000/73934/

用法：python3 scripts/build-earth-textures.py <源图目录>（需要 Pillow 与 numpy）
"""

import os
import sys

import numpy as np
from PIL import Image

Image.MAX_IMAGE_PIXELS = None
SIZE = (4096, 2048)
# world.200407 的海洋是统一的深蓝，用它抠出水体遮罩（海面高光只打在水上）
OCEAN_RGB = np.array([2, 5, 20])


def main(source: str) -> None:
    out = os.path.join(os.path.dirname(__file__), "..", "public", "textures", "earth")
    os.makedirs(out, exist_ok=True)
    src = lambda name: os.path.join(source, name)

    Image.open(src("world.topo.bathy.200410.3x21600x10800.jpg")).resize(SIZE, Image.LANCZOS).save(
        os.path.join(out, "day-4k.webp"), "WEBP", quality=88, method=6
    )
    Image.open(src("BlackMarble_2016_3km.jpg")).resize(SIZE, Image.LANCZOS).save(
        os.path.join(out, "night-4k.webp"), "WEBP", quality=88, method=6
    )
    Image.open(src("cloud_combined_8192.tif")).convert("L").convert("RGB").resize(SIZE, Image.LANCZOS).save(
        os.path.join(out, "clouds-4k.webp"), "WEBP", quality=82, method=6
    )

    world = np.asarray(Image.open(src("world.200407.3x21600x10800.jpg")), dtype=np.int16)
    water = (np.abs(world - OCEAN_RGB).sum(axis=2) < 18).astype(np.uint8) * 255
    del world
    Image.fromarray(water).resize(SIZE, Image.BOX).save(os.path.join(out, "water-4k.png"), optimize=True)

    Image.open(src("gebco_08_rev_elev_21600x10800.png")).resize(SIZE, Image.LANCZOS).save(
        os.path.join(out, "height-4k.jpg"), "JPEG", quality=90, optimize=True, progressive=True
    )


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else ".")
