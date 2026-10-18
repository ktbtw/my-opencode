#!/usr/bin/env python3
"""悬浮助手自动化自测。

screencap 输出的是旋转 90° 的图（2712x1220），而 input 坐标系是 1220x2712，
所以截图坐标 (sx, sy) 对应 input (sy, 2712 - sx)。本脚本统一按这个换算点击，
并用 dumpsys input 的 frame 判断当前处于球 / 窗 / 全屏哪种形态。
"""

import re
import subprocess
import sys
import time

SERIAL = "bc2ff1cd"
SCREEN_W = 2712
PKG = "com.chatcodex.chat_codex_app"
SHOT = "/tmp/overlay_shot.png"


def adb(*args):
    """统一返回 bytes，避免 screencap 的二进制输出被当文本解码。"""
    return subprocess.run(
        ["adb", "-s", SERIAL, *args],
        capture_output=True,
    ).stdout


def frame():
    out = adb(
        "shell",
        "dumpsys input 2>/dev/null | grep 'com.chatcodex.chat_codex_app,' | head -1",
    ).decode("utf-8", "ignore")
    match = re.search(r"frame=\[(\d+),(\d+)\]\[(\d+),(\d+)\]", out)
    if not match:
        return None
    return tuple(int(v) for v in match.groups())


def shape():
    box = frame()
    if box is None:
        return "none"
    x1, y1, x2, y2 = box
    w, h = x2 - x1, y2 - y1
    if w == h and w < 600:
        return f"ball({w})"
    if w >= 1200 or h >= 2600:
        return f"fullscreen({w}x{h})"
    return f"window({w}x{h})"


def shot_size():
    """读取当前截图尺寸，判断是否需要旋转映射。"""
    data = adb("exec-out", "screencap", "-p")
    import struct

    if len(data) < 24 or data[:8] != b"\x89PNG\r\n\x1a\n":
        return None
    width, height = struct.unpack(">II", data[16:24])
    return width, height


def tap_shot(sx, sy, note=""):
    """截图坐标 -> input 坐标。竖屏直接对应，横屏需要旋转映射。"""
    size = shot_size()
    if size and size[0] > size[1]:
        # 截图被旋转 90°：input (sy, W - sx)
        ix, iy = sy, size[0] - sx
    else:
        ix, iy = sx, sy
    adb("shell", "input", "tap", str(ix), str(iy))
    time.sleep(2.5)
    print(f"  点击 {note or (sx, sy)} -> ({ix},{iy}) -> {shape()}")


def shot(path=SHOT):
    with open(path, "wb") as handle:
        handle.write(adb("exec-out", "screencap", "-p"))
    print(f"  截图 {path}")


def restart():
    adb("shell", "am", "force-stop", PKG)
    time.sleep(1)
    adb("shell", "am", "start", "-n", f"{PKG}/.MainActivity")
    time.sleep(9)
    print(f"  重启后: {shape()}")


def flow():
    restart()
    # 球在截图右侧中部；点击后应变 window
    tap_shot(2436, 862, "球")
    shot("/tmp/overlay_window.png")
    # 窗内 ∨（截图 1056,323）应切全屏
    tap_shot(1056, 323, "∨ 全屏")
    shot("/tmp/overlay_fullscreen.png")
    # 全屏下拉关闭：从顶部往下拖
    adb("shell", "input", "swipe", "600", "500", "600", "1200", "300")
    time.sleep(2)
    print(f"  下拉后: {shape()}")
    shot("/tmp/overlay_after_swipe.png")


if __name__ == "__main__":
    action = sys.argv[1] if len(sys.argv) > 1 else "flow"
    if action == "shape":
        print(shape())
    elif action == "flow":
        flow()
    elif action == "restart":
        restart()
