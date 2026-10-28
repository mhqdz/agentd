#!/usr/bin/env python3
"""常驻插件示例：JSON-RPC 2.0 over stdio（行分隔）。

暴露两个工具：
  demo_now    当前时间（带进程启动时间与调用计数，用来验证"同一个进程"）
  demo_echo   回显 message
"""
import json, sys, time

STARTED = time.strftime("%H:%M:%S")
CALLS = 0

def handle(method, params):
    global CALLS
    if method == "initialize":
        return {"ok": True, "protocolVersion": "1", "started": STARTED}
    if method == "tools/list":
        return {"tools": [
            {"name": "demo_now", "description": "返回当前时间、插件启动时间与累计调用次数（用于验证常驻进程）",
             "params": {}, "required": []},
            {"name": "demo_echo", "description": "原样回显 message",
             "params": {"message": {"type": "string", "description": "要回显的文字"}},
             "required": ["message"]},
        ]}
    if method == "tools/call":
        CALLS += 1
        name = params.get("name")
        args = params.get("arguments") or {}
        if name == "demo_now":
            text = "now=%s plugin_started=%s calls=%d" % (time.strftime("%H:%M:%S"), STARTED, CALLS)
            return {"content": [{"type": "text", "text": text}]}
        if name == "demo_echo":
            return {"content": [{"type": "text", "text": "echo: " + str(args.get("message", ""))}]}
        raise ValueError("unknown tool: %s" % name)
    raise ValueError("unknown method: %s" % method)

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        req = json.loads(line)
        rid = req.get("id")
        try:
            result = handle(req.get("method"), req.get("params") or {})
            out = {"jsonrpc": "2.0", "id": rid, "result": result}
        except Exception as e:
            out = {"jsonrpc": "2.0", "id": rid, "error": {"code": -32000, "message": str(e)}}
        sys.stdout.write(json.dumps(out, ensure_ascii=False) + "\n")
        sys.stdout.flush()
    except Exception:
        continue
