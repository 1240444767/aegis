"""生成概览页「宇宙」彩蛋的资源（public/cosmos/）。

源数据（先下载到同一目录）：
  starmap_2020_8k_gal.exr  NASA SVS Deep Star Maps 2020，银道坐标全天星图（公有领域）
                           https://svs.gsfc.nasa.gov/vis/a000000/a004800/a004851/
  PIA10748.jpg             NASA/JPL-Caltech/R. Hurt 银河系俯视结构图
                           https://images-assets.nasa.gov/image/PIA10748/PIA10748~orig.jpg
  2mrs_v240.tgz            2MASS 红移巡天（Huchra et al. 2012）43,533 个星系
                           http://tdc-www.harvard.edu/2mrs/2mrs_v240.tgz
  wmap_ilc_9yr_v5.fits     WMAP 九年 ILC 宇宙微波背景全天图（HEALPix NSIDE 512，NESTED，银道坐标）
                           https://lambda.gsfc.nasa.gov/data/map/dr5/dfp/ilc/wmap_ilc_9yr_v5.fits
需要 ImageMagick（读 EXR）、Pillow 与 numpy。

用法：python3 scripts/build-cosmos-assets.py <源数据目录>

全天图统一为等距圆柱投影、银道坐标：图像中心为 l=0、b=0，经度向左增大（从球内看天空的习惯）。
"""

import io
import os
import struct
import subprocess
import sys
import tarfile

import numpy as np
from PIL import Image

Image.MAX_IMAGE_PIXELS = None
H0 = 70.0  # km/s/Mpc


def out_dir() -> str:
    path = os.path.join(os.path.dirname(__file__), "..", "public", "cosmos")
    os.makedirs(path, exist_ok=True)
    return path


def build_sky(source: str, out: str) -> None:
    """EXR 是线性辐射亮度；1 - exp(-4x) 压高光，再做 sRGB 伽马。"""
    raw = os.path.join(source, "sky8k.raw")
    subprocess.run(
        ["magick", os.path.join(source, "starmap_2020_8k_gal.exr"), "-depth", "32",
         "-define", "quantum:format=floating-point", f"RGB:{raw}"],
        check=True,
    )
    linear = np.fromfile(raw, dtype=np.float32).reshape(4096, 8192, 3)
    os.remove(raw)
    mapped = (np.clip(1.0 - np.exp(-linear * 4.0), 0.0, 1.0) ** (1 / 2.2) * 255.0 + 0.5).astype(np.uint8)
    del linear
    image = Image.fromarray(mapped)
    image.save(os.path.join(out, "sky-8k.webp"), "WEBP", quality=74, method=6)
    image.resize((4096, 2048), Image.LANCZOS).save(os.path.join(out, "sky-4k.webp"), "WEBP", quality=86, method=6)


def build_milky_way(source: str, out: str) -> None:
    Image.open(os.path.join(source, "PIA10748.jpg")).convert("RGB").resize((4096, 4096), Image.LANCZOS).save(
        os.path.join(out, "milkyway-4k.webp"), "WEBP", quality=86, method=6
    )


def build_2mrs(source: str, out: str) -> None:
    """银道直角坐标（Mpc，x 指向银心、z 指向北银极），距离取 cz / H0。

    文件格式（小端）：uint32 数量；int16×3×N 坐标（单位 0.02 Mpc）；uint8×N 的 Ks 星等×10。
    """
    with tarfile.open(os.path.join(source, "2mrs_v240.tgz")) as archive:
        text = archive.extractfile("catalog/2mrs_1175_done.dat").read().decode("ascii", "replace")
    rows = []
    for line in text.splitlines():
        if not line or line.startswith("#"):
            continue
        cols = line.split()
        l, b, k, v = float(cols[3]), float(cols[4]), float(cols[5]), float(cols[24])
        if v <= 0:
            continue  # 本星系群内的蓝移星系，按哈勃定律没有意义
        rows.append((l, b, k, v / H0))
    data = np.array(rows, dtype=np.float64)
    l, b = np.radians(data[:, 0]), np.radians(data[:, 1])
    d = data[:, 3]
    xyz = np.stack([d * np.cos(b) * np.cos(l), d * np.cos(b) * np.sin(l), d * np.sin(b)], axis=1)
    quantized = np.clip(np.round(xyz / 0.02), -32767, 32767).astype("<i2")
    mags = np.clip(np.round(data[:, 2] * 10), 0, 255).astype(np.uint8)
    with open(os.path.join(out, "2mrs.bin"), "wb") as handle:
        handle.write(struct.pack("<I", len(rows)))
        handle.write(quantized.tobytes())
        handle.write(mags.tobytes())
    print(f"2mrs galaxies: {len(rows)}, max distance {d.max():.0f} Mpc")


def _spread_bits(value: np.ndarray) -> np.ndarray:
    result = np.zeros_like(value)
    for bit in range(16):
        result |= ((value >> bit) & 1) << (2 * bit)
    return result


def ang2pix_nest(nside: int, theta: np.ndarray, phi: np.ndarray) -> np.ndarray:
    """HEALPix NESTED 方案的 ang2pix（与 healpy 同一算法的向量化实现）。"""
    z = np.cos(theta)
    za = np.abs(z)
    tt = np.mod(phi, 2 * np.pi) / (np.pi / 2)  # [0, 4)

    face = np.zeros(theta.shape, dtype=np.int64)
    ix = np.zeros(theta.shape, dtype=np.int64)
    iy = np.zeros(theta.shape, dtype=np.int64)

    eq = za <= 2.0 / 3.0
    t1 = nside * (0.5 + tt[eq])
    t2 = nside * (z[eq] * 0.75)
    jp = np.floor(t1 - t2).astype(np.int64)
    jm = np.floor(t1 + t2).astype(np.int64)
    ifp, ifm = jp // nside, jm // nside
    face[eq] = np.where(ifp == ifm, ifp | 4, np.where(ifp < ifm, ifp, ifm + 8))
    ix[eq] = jm & (nside - 1)
    iy[eq] = nside - (jp & (nside - 1)) - 1

    pol = ~eq
    ntt = np.minimum(np.floor(tt[pol]).astype(np.int64), 3)
    tp = tt[pol] - ntt
    tmp = nside * np.sqrt(3 * (1 - za[pol]))
    jp = np.minimum(np.floor(tp * tmp).astype(np.int64), nside - 1)
    jm = np.minimum(np.floor((1 - tp) * tmp).astype(np.int64), nside - 1)
    north = z[pol] > 0
    face[pol] = np.where(north, ntt, ntt + 8)
    ix[pol] = np.where(north, nside - jm - 1, jp)
    iy[pol] = np.where(north, nside - jp - 1, jm)

    return face * nside * nside + _spread_bits(ix) + 2 * _spread_bits(iy)


def read_wmap(path: str) -> tuple[np.ndarray, int]:
    data = open(path, "rb").read()
    offset = 0
    headers = []
    while len(headers) < 2:
        header = {}
        while True:
            block = data[offset:offset + 2880]
            offset += 2880
            done = False
            for i in range(0, 2880, 80):
                card = block[i:i + 80].decode("ascii")
                key = card[:8].strip()
                if key == "END":
                    done = True
                    break
                if card[8:10] == "= ":
                    header[key] = card[10:].split("/")[0].strip().strip("'").strip()
            if done:
                break
        headers.append(header)
        if len(headers) == 1:
            continue  # 主头没有数据
    table = headers[1]
    rows, width = int(table["NAXIS2"]), int(table["NAXIS1"])
    records = np.frombuffer(data[offset:offset + rows * width], dtype=">f4").reshape(rows, width // 4)
    return records[:, 0].astype(np.float32), int(table["NSIDE"])


# 接近 Planck 发布图的冷暖配色：深蓝 → 青白 → 橙红
CMB_STOPS = [
    (0.00, (0, 0, 255)), (0.17, (0, 112, 255)), (0.33, (0, 221, 255)), (0.50, (255, 237, 217)),
    (0.67, (255, 180, 0)), (0.83, (255, 75, 0)), (1.00, (100, 0, 0)),
]


def build_cmb(source: str, out: str) -> None:
    temperature, nside = read_wmap(os.path.join(source, "wmap_ilc_9yr_v5.fits"))
    width, height = 4096, 2048
    xs = (np.arange(width) + 0.5) / width
    ys = (np.arange(height) + 0.5) / height
    lon = np.radians((0.5 - xs) * 360.0)  # 中心 l=0，向左增大
    theta = ys * np.pi                      # 余纬
    phi_grid, theta_grid = np.meshgrid(lon, theta)
    pix = ang2pix_nest(nside, theta_grid.ravel(), phi_grid.ravel())
    values = temperature[pix].reshape(height, width)
    t = np.clip(values / 0.5 + 0.5, 0.0, 1.0)  # ±0.25 mK
    stops = np.array([s[0] for s in CMB_STOPS])
    colors = np.array([s[1] for s in CMB_STOPS], dtype=np.float32)
    rgb = np.stack([np.interp(t, stops, colors[:, c]) for c in range(3)], axis=-1).astype(np.uint8)
    Image.fromarray(rgb).save(os.path.join(out, "cmb-4k.webp"), "WEBP", quality=88, method=6)


def build_cosmic_web(out: str, n: int = 96, seed: int = 7) -> None:
    """泽尔多维奇近似：平滑后的高斯随机场给出位移，粒子沿位移聚成墙、纤维与结点。

    功率谱 P(k) ∝ k^-1，按 2 格高斯平滑；线性密度涨落 σ 取 2.5，足以出现壳层交叉。
    盒子边长 6 Gly，切成半径 3 Gly 的球；半径 0.9 Gly 以内留给 2MRS 的真实星系。
    随机保留 35% 的粒子控制体积。文件格式：uint32 数量；int16×3×N 坐标（单位 0.0001 Gly）。
    """
    rng = np.random.default_rng(seed)
    ki = np.fft.fftfreq(n) * n
    kx, ky, kz = np.meshgrid(ki, ki, ki, indexing="ij")
    kp = 2 * np.pi / n
    k2 = (kx**2 + ky**2 + kz**2) * kp * kp
    k2[0, 0, 0] = 1.0
    amplitude = k2 ** -0.5 * np.exp(-k2 * 2.0**2)
    amplitude[0, 0, 0] = 0.0
    delta = np.fft.fftn(rng.normal(size=k2.shape)) * np.sqrt(amplitude)
    divergence = np.real(np.fft.ifftn(-delta))
    displacement = [np.real(np.fft.ifftn(1j * (kk * kp) / k2 * delta)) for kk in (kx, ky, kz)]
    scale = 2.5 / np.std(divergence)
    grid = np.stack(np.meshgrid(np.arange(n), np.arange(n), np.arange(n), indexing="ij"), axis=-1).reshape(-1, 3)
    jitter = rng.uniform(-0.3, 0.3, size=grid.shape)  # 打散网格残留的摩尔纹
    moved = grid + jitter + scale * np.stack([d.ravel() for d in displacement], axis=1)
    pos = (np.mod(moved, n) / n - 0.5) * 6.0  # Gly
    r = np.linalg.norm(pos, axis=1)
    keep = (r <= 3.0) & (r >= 0.9) & (rng.random(len(pos)) < 0.35)
    pos = pos[keep]
    quantized = np.clip(np.round(pos / 0.0001), -32767, 32767).astype("<i2")
    with open(os.path.join(out, "cosmic-web.bin"), "wb") as handle:
        handle.write(struct.pack("<I", len(pos)))
        handle.write(quantized.tobytes())
    print(f"cosmic web particles: {len(pos)}")


def main(source: str) -> None:
    out = out_dir()
    build_sky(source, out)
    build_milky_way(source, out)
    build_2mrs(source, out)
    build_cmb(source, out)
    build_cosmic_web(out)


if __name__ == "__main__":
    main(sys.argv[1] if len(sys.argv) > 1 else ".")
