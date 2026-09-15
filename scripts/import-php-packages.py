#!/usr/bin/env python3
"""导入用户确认的菲律宾 App Store 月付套餐；默认只预览，--apply 才写入。"""
import argparse
import json
import os
import sys
import urllib.error
import urllib.request
from decimal import Decimal, ROUND_HALF_UP

PACKAGES = [("Plus", "plus", 99900), ("5X", "pro_5x", 649000), ("20X", "pro_20x", 999000)]
VAT_RATE = Decimal("0.12")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--origin", default="http://localhost:15680")
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    origin = args.origin.rstrip("/")

    def request(path, data=None, token=None, method=None):
        headers = {"Content-Type": "application/json", "X-Aitok-Page": "/admin/packages"}
        if token:
            headers["Authorization"] = "Bearer " + token
        req = urllib.request.Request(origin + "/api" + path, data=json.dumps(data).encode() if data is not None else None, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=20) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            raise RuntimeError(f"{path}: HTTP {error.code}，未继续导入") from None

    token = os.environ.get("AITOK_ADMIN_TOKEN")
    if not token:
        password = os.environ.get("ADMIN_PASSWORD")
        if not password:
            raise RuntimeError("请通过环境提供 AITOK_ADMIN_TOKEN 或 ADMIN_USERNAME / ADMIN_PASSWORD")
        token = request("/login", {"username": os.environ.get("ADMIN_USERNAME", "admin"), "password": password})["token"]
    data = request("/packages", token=token)
    if not data.get("can_manage"):
        raise RuntimeError("当前账号没有套餐管理权限")
    rate = data.get("exchange_rate")
    if not rate or not data.get("exchange_rate_fresh"):
        raise RuntimeError("每日汇率尚未就绪，请先部署汇率功能并等待同步成功")
    planned = []
    for label, plan, gross_amount in PACKAGES:
        # 先把含税 PHP 分还原为未税 PHP 分，再按当日汇率折算 USD 分。
        amount = int((Decimal(gross_amount) / (1 + VAT_RATE)).quantize(Decimal("1"), rounding=ROUND_HALF_UP))
        name = "ChatGPT " + label + " · 菲律宾 App Store（未税）"
        matches = [p for p in data["packages"] if p["name"] == name and p["plan"] == plan and p["region"] == "PH"]
        if len(matches) > 1:
            raise RuntimeError("存在重名套餐，请先人工核对：" + name)
        usd = int((Decimal(amount) * Decimal(rate["rate"])).quantize(Decimal("1"), rounding=ROUND_HALF_UP))
        body = {"name": name, "plan": plan, "region": "PH", "currency": "PHP", "original_amount_minor": amount, "sale_usd_minor": usd, "wallet_tokens": 0, "months": 1, "enabled": True, "auto_usd": True, "notes": f"来源：用户确认的菲律宾 App Store 套餐价格截图（2026-09-15），月付。截图含税价 PHP {gross_amount / 100:.2f}；增值税 12%；未税价 = 含税价 ÷ 1.12 = PHP {amount / 100:.2f}，四舍五入到分。按用户要求使用未税 PHP 价格每日折算 USD；不代表网页版结账价格或实际银行卡扣款。"}
        previous = matches[0] if matches else None
        changed = previous is None or any(previous.get(k) != v for k, v in body.items() if k != "sale_usd_minor")
        planned.append((body, previous, changed))
        print(f"{label}: 含税 PHP {gross_amount / 100:.2f} ÷ 1.12 = 未税 PHP {amount / 100:.2f} → USD {usd / 100:.2f} / 月（{'新增' if previous is None else '更新' if changed else '已一致'}）")
    print(f"汇率：1 PHP = {rate['rate']} USD；同步于 {rate['synced_at']}")
    if not args.apply:
        print("仅预览，未写入；加 --apply 执行。")
        return
    for body, previous, changed in planned:
        if changed:
            path = "/packages" + ("/" + str(previous["id"]) if previous else "")
            request(path, body, token, "PATCH" if previous else "POST")
    saved = request("/packages", token=token)["packages"]
    for body, _, _ in planned:
        matching = [p for p in saved if p["name"] == body["name"] and p["plan"] == body["plan"] and p["region"] == "PH"]
        if len(matching) != 1 or any(matching[0].get(k) != v for k, v in body.items() if k != "sale_usd_minor"):
            raise RuntimeError("导入后核对失败：" + body["name"])
    print("三个套餐已导入并核对，操作审计已由后端记录。")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, urllib.error.URLError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
