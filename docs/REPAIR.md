# agentd 修复记录

上游代理 400（Bad request syntax）
- 现象：curl 正常，但应用里报 400，错误里能看到 "{...}POST /180/v1/chat/completions HTTP/1.1" 这种拼接。
- 原因：本地 llm-proxy 对 keep-alive 复用连接处理有误。
- 修法：客户端禁用复用：http.Transport{DisableKeepAlives:true, DisableCompression:true} + req.Close = true。

改了 CSS 但浏览器里看不到该属性
- 现象：写了 grid-template-columns，DevTools 里根本没有这一项。
- 原因：同一元素上 grid-template-areas 的列数与 grid-template-columns 不一致时，浏览器判定整条 grid 声明非法并丢弃。
- 排查：先看 computed 是否为空，再回头数区域列数；不要继续追加覆盖。

!important 压过内联样式
- 现象：JS 里 element.style.height = ... 不生效。
- 原因：样式表的 !important 优先级高于内联（无 !important）声明。
- 修法：去掉样式表里的 !important，或改用 CSS 变量（JS 写 --h，样式里 height:var(--h)）。

面板能看见但点不动
- 原因：给祖先元素加 z-index 会形成层叠上下文，子元素再高的 z-index 也出不来，被后绘制的遮罩盖住。
- 修法：不要给容器乱加 z-index；只在需要压层级的两者之间排定。

字体不一致导致"没垂直居中"
- 现象：同一行文字看着基线不齐。
- 原因：endfield 的 .ef-read / .ef-prose 自带 --ef-font-* 变量，回落到 Noto，与全局字体不同。
- 修法：把 --ef-font-ui / --ef-font-text / --ef-font-mono 也指向同一字体。
