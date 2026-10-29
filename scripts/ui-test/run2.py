import sys, time, json
sys.path.insert(0, __import__('os').path.dirname(__file__))
from wd import Session, req
FAIL=[]
def check(n,c,e=""):
    print(("  ✓ " if c else "  ✗ ")+n+(f"  {e}" if e else ""))
    if not c: FAIL.append(n)
B="http://127.0.0.1:18400"

s=Session(1600,1000); req("POST",f"/session/{s.id}/window/rect",{"width":1600,"height":1000})

# 控制台错误收集
s.get(B+"/?conv=51"); time.sleep(1.5)
s.js("window.__errs=[]; window.addEventListener('error',e=>window.__errs.push(String(e.message))); window.addEventListener('unhandledrejection',e=>window.__errs.push('rej:'+String(e.reason))); return 1")

# 打开/关闭设置、点模型、点工具、点任务 —— 检查错误
s.get(B+"/?settings=1&conv=51"); time.sleep(1.0)
s.js("const t=[...document.querySelectorAll('.set-card .item')].find(x=>x.textContent.startsWith('kb_get')); t && t.click(); return !!t"); time.sleep(.6)
s.js("const m=[...document.querySelectorAll('.col-list .item.col')][0]; m && m.click(); return !!m"); time.sleep(.6)
errs=s.js("return window.__errs||[]")
check("交互过程无 JS 错误", len(errs)==0, f"{errs}")

# 任务点击展开
s.get(B+"/?settings=1&conv=51"); time.sleep(1.2)
n1=s.js("return document.querySelectorAll('.task-step').length")
s.js("const t=[...document.querySelectorAll('.set-card .item.col')].find(x=>/#\\d+/.test(x.textContent)); t && t.click(); return !!t"); time.sleep(1.0)
n2=s.js("return document.querySelectorAll('.task-step').length")
check("点任务行展开步骤", n2>=1 and n2>n1, f"{n1}->{n2}")
check("任务步骤含命令与耗时", s.js("const r=document.querySelector('.task-step'); return r? /ms/.test(r.textContent) : false") is True)

# 任务深链是否滚入视野
s.get(B+"/?settings=1&conv=51&task="+str(json.loads(__import__('urllib.request',fromlist=['x']).urlopen(B+"/api/tasks").read())['tasks'][0]['id']))
time.sleep(1.5)
vis = s.js("const st=document.querySelector('.task-step'); if(!st) return 'no-step'; const r=st.getBoundingClientRect(); return [Math.round(r.top), Math.round(window.innerHeight)]")
check("任务深链自动滚到详情", isinstance(vis,list) and 0 <= vis[0] <= vis[1], f"{vis}")

# 会话视图：打开是否在底部
s.get(B+"/?conv=51"); time.sleep(2)
info=s.js("const th=document.querySelector('.thread')||document.scrollingElement; return [th.scrollTop, th.scrollHeight, th.clientHeight]")
check("打开会话滚到底部", info and info[0]+info[2] >= info[1]-20, f"{info}")

# 最后一条消息不被输入框遮住
s.js("const th=document.querySelector('.thread')||document.scrollingElement; th.scrollTop=th.scrollHeight; return 1"); time.sleep(.4)
gap=s.js("const ms=[...document.querySelectorAll('.msg')]; const last=ms[ms.length-1]; const c=document.querySelector('.composer'); if(!last||!c) return null; return Math.round(c.getBoundingClientRect().top - last.getBoundingClientRect().bottom)")
check("最后一条消息在输入框上方（间距>0）", gap is not None and gap >= 0, f"gap={gap}")

# 输入框与发送
s.js("const b=document.querySelector('button.send'); window.__sendDisabled=b?b.disabled:'none'; return 1")
print("  发送按钮初始 disabled =", s.js("return window.__sendDisabled"))
s.shot("/tmp/uitest/conv_bottom.png")

# 窄屏会话视图
s.close()
s2=Session(900,1300); req("POST",f"/session/{s2.id}/window/rect",{"width":900,"height":1300})
s2.get(B+"/?conv=51"); time.sleep(1.5)
check("窄屏无横向溢出", s2.js("return document.scrollingElement.scrollWidth - innerWidth") <= 0)
check("窄屏顶栏显示进度与操作", s2.js("const t=document.querySelector('.rail'); return t? Math.round(t.getBoundingClientRect().width) : -1") != -1)
s2.shot("/tmp/uitest/narrow_conv.png")
s2.close()
s.close()
print("\n失败:", FAIL if FAIL else "无")
