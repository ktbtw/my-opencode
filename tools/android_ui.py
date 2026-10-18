#!/usr/bin/env python3
"""Android 端 UI 自动化小工具：按文本/content-desc 定位并点击，用于自测流程。

用法:
    python3 tools/android_ui.py dump                 # 打印当前界面元素
    python3 tools/android_ui.py tap "我的windows"     # 按文本或描述点击
    python3 tools/android_ui.py shot /tmp/a.png      # 截图
"""
import re
import subprocess
import sys
import time
import xml.etree.ElementTree as ET

SERIAL = "114.66.31.184:39451"


def adb(*args, timeout=60):
    return subprocess.run(
        ["adb", "-s", SERIAL, *args], capture_output=True, text=True, timeout=timeout
    )


def dump():
    adb("shell", "uiautomator", "dump", "/sdcard/ui.xml")
    adb("pull", "/sdcard/ui.xml", "/tmp/ui.xml")
    return ET.parse("/tmp/ui.xml").getroot()


def nodes():
    for node in dump().iter("node"):
        text = node.get("text") or ""
        desc = node.get("content-desc") or ""
        if text or desc:
            yield node, text, desc


def center(node):
    x1, y1, x2, y2 = map(int, re.findall(r"-?\d+", node.get("bounds")))
    return (x1 + x2) // 2, (y1 + y2) // 2


def find(target):
    for node, text, desc in nodes():
        if target in text or target in desc:
            return node
    return None


def tap(target):
    node = find(target)
    if node is None:
        print("  ✗ 找不到:", target)
        return False
    x, y = center(node)
    adb("shell", "input", "tap", str(x), str(y))
    print("  ✓ 点击", target, "at", x, y)
    return True


def tap_at(node, dx=0, dy=0):
    """按节点左上角偏移点击：合并语义节点时，中心可能落在子按钮上。"""
    x1, y1, _, _ = map(int, re.findall(r"-?\d+", node.get("bounds")))
    x, y = x1 + dx, y1 + dy
    adb("shell", "input", "tap", str(x), str(y))
    print("  ✓ 点击偏移 at", x, y)
    return True


def tap_title(target, dx=180, dy=60):
    node = find(target)
    if node is None:
        print("  ✗ 找不到:", target)
        return False
    return tap_at(node, dx, dy)


def shot(path):
    adb("shell", "screencap", "-p", "/sdcard/shot.png")
    adb("pull", "/sdcard/shot.png", path)
    print("  截图:", path)


def main():
    cmd = sys.argv[1] if len(sys.argv) > 1 else "dump"
    if cmd == "dump":
        for node, text, desc in nodes():
            print(" ", repr(text)[:40], "|", repr(desc)[:40], "|", node.get("bounds"))
    elif cmd == "tap":
        tap(sys.argv[2])
    elif cmd == "tap-title":
        tap_title(sys.argv[2])
    elif cmd == "shot":
        shot(sys.argv[2] if len(sys.argv) > 2 else "/tmp/shot.png")
    elif cmd == "flow":
        run_flow()
    else:
        print("unknown:", cmd)


def run_flow():
    """完整流程：设备列表 → 设备详情 → 语义 Agent → 选择 → 应用并重启。"""
    device = sys.argv[2] if len(sys.argv) > 2 else "我的windows"
    agent = sys.argv[3] if len(sys.argv) > 3 else "逆向专家-安卓"

    print("[1] 打开设备:", device)
    tap_title(device)
    time.sleep(3)

    print("[2] 打开语义 Agent（图标在标签下方约 370px 处）")
    node = find("语义 Agent")
    if node is None:
        print("  ✗ 找不到语义 Agent 标签")
        return 1
    x1, y1, _, _ = map(int, re.findall(r"-?\d+", node.get("bounds")))
    adb("shell", "input", "tap", str(x1 + 24), str(y1 + 368))
    time.sleep(3)

    print("[3] 选择 Agent:", agent)
    if not tap(agent):
        return 1
    time.sleep(1)

    print("[4] 应用并重启")
    if not tap("应用并重启"):
        return 1

    print("[5] 等待预检完成")
    for _ in range(15):
        time.sleep(1)
        if find("切换检测"):
            break
    time.sleep(6)
    shot("/tmp/flow_done.png")
    for target in ("正在切换", "3%", "100%"):
        node = find(target)
        print("  界面包含", target, ":", node is not None)
    return 0


if __name__ == "__main__":
    sys.exit(main() or 0)
