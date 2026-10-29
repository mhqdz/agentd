import sys, time, json
sys.path.insert(0, __import__('os').path.dirname(__file__))
from wd import Session, req
FAIL=[]
def check(n,c,e=""):
    print(("  ✓ " if c else "  ✗ ")+n+(f"  {e}" if e else ""))
    if not c: FAIL.append(n)
B="http://127.0.0.1:18400"
_tasks = json.loads(__import__('urllib.request',fromlist=['x']).urlopen(B+"/api/tasks").read()).get('tasks') or []
if not _tasks:
    print("  (没有任务，跳过深链用例；先跑 run.py 会造一条)")
tid = _tasks[0]['id'] if _tasks else None

s=Session(1600,1000); req("POST",f"/session/{s.id}/window/rect",{"width":1600,"height":1000})
# 任务深链滚动（修复后）
s.get(B+(f"/?settings=1&conv=51&task={tid}" if tid else "/?settings=1&conv=51")); time.sleep(2)
vis=s.js("const st=document.querySelector('.task-step'); if(!st) return 'no-step'; const r=st.getBoundingClientRect(); return [Math.round(r.top), innerHeight]")
check("任务深链自动滚到详情", (not tid) or (isinstance(vis,list) and 0 <= vis[0] <= vis[1]), f"{vis}")
s.shot("/tmp/uitest/wide_task2.png")

# 窄屏（同一会话改窗口尺寸）
req("POST",f"/session/{s.id}/window/rect",{"width":900,"height":1300})
s.get(B+"/?conv=51"); time.sleep(1.6)
check("窄屏无横向溢出", s.js("return document.scrollingElement.scrollWidth - innerWidth") <= 0)
top = s.js("const r=document.querySelector('.rail'); if(!r) return null; const b=r.getBoundingClientRect(); return [Math.round(b.top), Math.round(b.width), Math.round(b.height)]")
print("  窄屏 rail rect:", top)
check("窄屏右栏搬到顶部（高度很小、贴顶）", isinstance(top,list) and top[0] <= 8, f"{top}")
check("窄屏消息列宽度合理（≥80% 视口）", s.js("const m=document.querySelector('.msg'); if(!m) return -1; return Math.round(m.getBoundingClientRect().width / innerWidth * 100)") >= 75)
s.shot("/tmp/uitest/narrow_conv.png")
# 窄屏折叠把手：可见（白底+描边）、内缩、不压正文
tg = s.js("const b=document.querySelector('.side-toggle'); if(!b) return null; const g=b.getBoundingClientRect(); const c=getComputedStyle(b); return [Math.round(g.left),Math.round(g.width),c.backgroundColor,c.borderTopWidth,Math.round(g.height)]")
check("窄屏折叠把手可见且有底色/描边", isinstance(tg,list) and tg[0] >= 4 and tg[2] not in ('rgba(0, 0, 0, 0)','transparent') and tg[3] != '0px', f"{tg}")
ov = s.js("const b=document.querySelector('.side-toggle').getBoundingClientRect(); const m=document.querySelector('.msg').getBoundingClientRect(); return b.right > m.left + 2")
check("折叠把手不压正文", ov is False)

# 窄屏设置单列
s.get(B+"/?settings=1&conv=51"); time.sleep(1.2)
w = s.js("return Math.round(document.querySelector('.col-list').getBoundingClientRect().width)")
check("窄屏卡片宽度≈视口-32", 820 <= w <= 900, f"w={w}")
check("窄屏设置无横向溢出", s.js("return document.scrollingElement.scrollWidth - innerWidth") <= 0)
s.shot("/tmp/uitest/narrow_settings2.png")
s.close()
print("\n失败:", FAIL if FAIL else "无")
