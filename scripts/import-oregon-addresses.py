"""从指定页面使用的公开接口采集 Oregon 地址，生成可重复导入的 JSON 和 SQL。"""
import argparse
import json
import time
import subprocess
from datetime import datetime, timezone
from pathlib import Path

SOURCE = "https://www.meiguodizhi.com/usa-address/oregon"
ENDPOINT = "https://www.meiguodizhi.com/api/v1/dz"


def save_addresses(output, addresses):
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_suffix(output.suffix + ".tmp")
    temporary.write_text(json.dumps({"source_url": SOURCE, "collected_at": datetime.now(timezone.utc).isoformat(), "addresses": list(addresses.values())}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    temporary.replace(output)


def address_key(item):
    return tuple(item[field].casefold() for field in ("address_line1", "city", "state", "postal_code"))


def collect(count, output):
    addresses = {}
    if output.exists():
        saved = json.loads(output.read_text(encoding="utf-8"))
        if saved.get("source_url") != SOURCE:
            raise ValueError("已有采集文件来源不匹配")
        for item in saved["addresses"]:
            if item.get("state") != "OR" or item.get("country") != "US" or item.get("source_url") != SOURCE:
                raise ValueError("已有采集文件包含非 Oregon 来源地址")
            if item.get("full_name") and item.get("source_data"):
                addresses[address_key(item)] = item
        print(f"恢复 {len(addresses)} 条已采集地址", flush=True)
    if len(addresses) >= count:
        return list(addresses.values())[:count]
    for attempt in range(count * 20):
        try:
            result = subprocess.run(["curl", "--fail", "--silent", "--show-error", "--max-time", "25",
                ENDPOINT, "-H", "Content-Type: application/json", "-H", "Referer: " + SOURCE,
                "-H", "Origin: https://www.meiguodizhi.com", "-A", "AiTok-address-import/1.0",
                "--data", json.dumps({"city": "", "path": "/usa-address/oregon", "method": "refresh"})],
                capture_output=True, check=True)
            payload = json.loads(result.stdout)
            if payload.get("status") != "ok":
                raise ValueError("来源接口未返回地址")
            raw = payload["address"]
            item = {"address_line1": " ".join(raw.get("Address", "").split()), "address_line2": "",
                    "city": raw.get("City", "").strip(), "state": raw.get("State", "").strip(),
                    "postal_code": str(raw.get("Zip_Code", "")).strip(), "country": "US",
                    "full_name": raw.get("Full_Name", "").strip(), "source_data": raw,
                    "source_url": SOURCE, "source_key": raw.get("rowkey", "")}
            if not item["full_name"] or len(raw) < 30:
                raise ValueError("来源未返回完整资料")
            if item["state"] != "OR" or not all(item[key] for key in ("address_line1", "city", "postal_code")):
                raise ValueError("来源地址字段不完整或不属于 Oregon")
            key = address_key(item)
            if key not in addresses:
                addresses[key] = item
                save_addresses(output, addresses)
                print(f"已采集 {len(addresses)}/{count} 条去重地址", flush=True)
            if len(addresses) == count:
                return list(addresses.values())
        except Exception as error:
            print(f"第 {attempt + 1} 次采集失败（{type(error).__name__}），稍后重试", flush=True)
            time.sleep(2)
        time.sleep(0.3)
    raise RuntimeError(f"仅获得 {len(addresses)} 条唯一地址，进度已保存；重新运行可继续采集，未生成 SQL")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--count", type=int, default=100)
    parser.add_argument("--output", required=True)
    parser.add_argument("--sql", required=True)
    args = parser.parse_args()
    if not 1 <= args.count <= 100:
        parser.error("每次采集数量必须为 1 到 100")
    addresses = collect(args.count, Path(args.output))
    save_addresses(Path(args.output), {address_key(item): item for item in addresses})
    fields = list(addresses[0])
    quote = lambda value: "'" + (json.dumps(value, ensure_ascii=False) if isinstance(value, dict) else value).replace("'", "''") + "'"
    values = ",\n".join("(" + ",".join(quote(item[field]) for field in fields) + ")" for item in addresses)
    conflict = """ON CONFLICT (lower(address_line1), lower(address_line2), lower(city), lower(state), lower(postal_code), country)
DO UPDATE SET full_name=EXCLUDED.full_name,source_data=EXCLUDED.source_data,source_key=EXCLUDED.source_key,updated_at=NOW()
WHERE addresses.user_id IS NULL AND addresses.source_url=EXCLUDED.source_url
  AND addresses.full_name='' AND addresses.source_data='{}'::jsonb"""
    Path(args.sql).write_text("BEGIN;\nSET LOCAL lock_timeout = '5s';\nINSERT INTO addresses (" + ",".join(fields) + ") VALUES\n" + values + "\n" + conflict + ";\nCOMMIT;\n")
    print(f"完成：{len(addresses)} 条 Oregon 地址，已生成 JSON 和 SQL", flush=True)


if __name__ == "__main__":
    main()
