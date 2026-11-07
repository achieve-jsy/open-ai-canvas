# Adweave

面向商品广告的 AI 制作工作台：从广告需求与商品素材出发，经过**可确认的脚本版本**、**逐镜头费用批准**、**真实模型生成**、**人工采用**，最终组装时间线并导出竖屏 MP4。

> 本项目是开源项目 [影策（Open AI Canvas）](https://github.com/ddcat-ai/open-ai-canvas) 的二次开发分支，并非原创实现。上游版权与 MIT 许可完整保留，见 [LICENSE](LICENSE) 与 [NOTICE](NOTICE)。

## 这个分支做了什么

在影策已有的画布、任务队列、模型渠道、账务与 FFmpeg 时间线之上，新增独立的广告制作模块：

- **广告项目与不可变确认版本**：草稿可改，确认后冻结；改稿不会污染已确认的历史与已用素材。
- **逐镜头报价与显式批准**：报价不调用模型；只有携带已展示报价哈希的确认才真实提交并计费。报价过期或价格变化返回 409，要求重新确认。
- **真实生成与人工采用分离**：生成成功只产生候选，必须人工播放、试听、必要时设置裁剪起点后才进入成片。
- **组装与导出**：服务端重新探测真实音视频时长与尺寸，不信任上游标称值；缺镜头、视频过短、旁白超长明确拒绝，不用黑帧凑时长。
- **独立时间线与原组装分离**：手改时间线不会覆盖已保存的原始组装。
- **本地内网模型接入**：新增声明式协议插件 `aimeter-seedance`，对接 Ark 兼容的 `/v1/contents/generations/tasks`；支持内联图片传输，参考素材无需公网存储。

## 现状与边界

如实说明，便于判断可用范围：

- 已用真实模型完成 **3 镜头 × 5 秒竖屏广告**的端到端验证：报价 → 批准 → 生成 → 采用 → 组装 → 15 秒 1080×1920 MP4 导出。
- **「商品图 + 一句需求 → AI 自动策划并生成完整分镜」尚未实现**，目前分镜仍需人工编写，只完成了设计与方案讨论。
- 仅在本机环境验证，**没有生产规模、并发、成本与稳定性数据**，不宜称为已上线的生产系统。
- 示例成片存在商品一致性不足（额外出现吸管、薄荷叶）与字幕底板过大的问题，属于质量未达标，不是已完成验收。
- 仅使用本地开发数据与测试记账价格，不代表真实供应商成本核算。

## 快速开始

依赖 Go 1.25+、Bun、FFmpeg/ffprobe。

```bash
# 后端（开发数据放在被 Git 忽略的目录）
cd backend/agent-runtime/pi && npm install && cd ../../..
cd backend
CANVAS_BACKEND_DATA_DIR=../.local/project-workbench-debug go run ./cmd/server

# 前端
cd web && bun run dev
```

访问 `http://127.0.0.1:3000`。默认关闭公开注册；首个注册账号为管理员，需在后台配置模型渠道后才可生成。

内网模型网关需精确放行，不要整体关闭 SSRF 防护：

```bash
CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS=your-gateway.example.com go run ./cmd/server
```

## 目录

| 目录 | 说明 |
| --- | --- |
| `backend/` | Go + Gin + GORM，业务 API、任务队列、模型协议、账务与渲染 |
| `web/` | React 19 + TypeScript + Vite 工作台前端 |
| `docs/` | 专题文档，含广告生成与接口说明 |
| `plugin-packages/` | 声明式模型协议插件 |

广告模块说明见 `docs/content/docs/backend/advertising-generation.mdx`。

## 许可

MIT，Copyright (c) 2026 ddcat and Open AI Canvas contributors。
本分支的修改同样以 MIT 发布，原始版权声明保留。

## 贡献者与团队

感谢参与产品设计、开发、测试、内容和社区建设的成员。

<table>
<tr>
<td align="center" valign="top" width="33%">
  <img src="assets/user-sikongyue.png" alt="爱笑的毛毛虫" width="72" height="72"><br>
  <strong>爱笑的毛毛虫</strong><br>
  <sub>用户名：sikongyue</sub><br>
  <sub><a href="mailto:315515767@qq.com">315515767@qq.com</a></sub><br>
  <sub><em>正在啃 main 分支，争取下次 merge 的时候变成蝴蝶</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-delve.jpg" alt="delve-s" width="72" height="72"><br>
  <strong>delve-s</strong><br>
  <sub><a href="mailto:3013141136@qq.com">3013141136@qq.com</a></sub><br>
  <sub><em>我亦无他，惟手熟尔</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-CyrusAuyeung.jpg" alt="CyrusAuyeung" width="72" height="72"><br>
  <strong>CyrusAuyeung</strong><br>
  <sub><a href="mailto:cyrusauyeungho@gmail.com">cyrusauyeungho@gmail.com</a></sub><br>
  <sub><em>HKUST(GZ) UG</em></sub>
</td>
</tr>
<tr>
<td align="center" valign="top" width="33%">
  <img src="assets/user-nz.jpg" alt="奶大佬" width="72" height="72"><br>
  <strong>奶大佬</strong><br>
  <sub><a href="mailto:1304634970@qq.com">1304634970@qq.com</a></sub><br>
  <sub><em>人生就是要不断的探索</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-dyh.jpg" alt="dyh" width="72" height="72"><br>
  <strong>dyh</strong><br>
  <sub><a href="mailto:1613203335@qq.com">1613203335@qq.com</a></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-kyori.jpg" alt="kyori" width="72" height="72"><br>
  <strong>kyori</strong><br>
  <sub><a href="mailto:1771634408@qq.com">1771634408@qq.com</a></sub><br>
  <sub><em>励志成为未来最好用的画布仓库的贡献者</em></sub>
</td>
</tr>
<tr>
<td align="center" valign="top" width="33%">
  <img src="assets/user-bowen.jpg" alt="Bowen" width="72" height="72"><br>
  <strong>Bowen</strong><br>
  <sub><a href="mailto:admin@bowen.games">admin@bowen.games</a></sub><br>
  <sub><em>剑走偏峰，雷厉风行。</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-ken.jpg" alt="ken" width="72" height="72"><br>
  <strong>ken</strong><br>
  <sub><a href="mailto:2506802@qq.com">2506802@qq.com</a></sub><br>
  <sub><em>走自己的路</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-fish.png.jpg" alt="fish" width="72" height="72"><br>
  <strong>fish</strong><br>
  <sub><a href="mailto:cihai.sea@gmail.com">cihai.sea@gmail.com</a></sub><br>
  <sub><em>AI 界热于助人的拖油瓶</em></sub>
</td>
</tr>
<tr>
<td align="center" valign="top" width="33%">
  <img src="assets/user-QAyong.jpg" alt="QAyong" width="72" height="72"><br>
  <strong>QAyong</strong><br>
  <sub>ID / B站：QAyong</sub><br>
  <sub><a href="mailto:2110491559@qq.com">2110491559@qq.com</a></sub><br>
  <sub><em>AI 短剧合规，资产确权</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-K37ix.jpg" alt="_K37ix." width="72" height="72"><br>
  <strong>_K37ix.</strong><br>
  <sub><a href="mailto:2773843782@qq.com">2773843782@qq.com</a></sub><br>
  <sub><em>Making things that think</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-rou.jpg" alt="Rou" width="72" height="72"><br>
  <strong>Rou</strong><br>
  <sub><a href="mailto:rou325089@163.com">rou325089@163.com</a></sub><br>
  <sub><em>上善若水</em></sub>
</td>
</tr>
<tr>
<td align="center" valign="top" width="33%">
  <img src="assets/user-vv.jpg" alt="vv" width="72" height="72"><br>
  <strong>vv</strong><br>
  <sub>dy/xhs：荣灵</sub><br>
  <sub><a href="mailto:2838033228@qq.com">2838033228@qq.com</a></sub><br>
  <sub><em>就是水水</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-dominic1556.jpg" alt="Dominic1556" width="72" height="72"><br>
  <strong>Dominic1556</strong><br>
  <sub><a href="mailto:184026530@qq.com">184026530@qq.com</a></sub><br>
  <sub><em>Done is better than perfect</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-yuxi.jpg" alt="宇熙" width="72" height="72"><br>
  <strong>宇熙</strong><br>
  <sub><a href="mailto:53121904@qq.com">53121904@qq.com</a></sub><br>
  <sub><em>年轻的时候不狂，老了拿什么回忆</em></sub>
</td>
</tr>
<tr>
<td align="center" valign="top" width="33%">
  <img src="assets/user-yingzi.png" alt="影子" width="72" height="72"><br>
  <strong>影子</strong><br>
  <sub><a href="mailto:305818148@qq.com">305818148@qq.com</a></sub><br>
  <sub><em>年纪大佬才明白人要顺势而为。</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-ray.jpg" alt="Ray" width="72" height="72"><br>
  <strong>Ray</strong><br>
  <sub><a href="mailto:cnraylee@qq.com">cnraylee@qq.com</a></sub><br>
  <sub><em>AI时代的全栈落地工，欢迎找我聊需求</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-bjsg.jpg" alt="不见山谷" width="72" height="72"><br>
  <strong>不见山谷</strong><br>
  <sub>VV：yu170718</sub><br>
  <sub><a href="mailto:1762202553@qq.com">1762202553@qq.com</a></sub><br>
  <sub><em>空山不见人，但闻人语响</em></sub>
</td>
</tr>
<tr>
<td align="center" valign="top" width="33%">
  <img src="assets/user-yep.jpg" alt="yep" width="72" height="72"><br>
  <strong>yep</strong><br>
  <sub><a href="mailto:1239738103@qq.com">1239738103@qq.com</a></sub><br>
  <sub><em>思考，坚持</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-hamburger.jpg" alt="汉堡爸爸" width="72" height="72"><br>
  <strong>汉堡爸爸</strong><br>
  <sub>VV：jxs62888</sub><br>
  <sub><a href="mailto:309151651@qq.com">309151651@qq.com</a></sub><br>
  <sub><em>没什么大不了</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-bensharp.jpg" alt="nancy" width="72" height="72"><br>
  <strong>nancy</strong><br>
  <sub>VV：jiahezuiai</sub><br>
  <sub><a href="mailto:275008147@qq.com">275008147@qq.com</a></sub><br>
  <sub><em>在哪跌倒，就在哪睡一觉</em></sub>
</td>
</tr>
<tr>
<td align="center" valign="top" width="33%">
  <img src="assets/user-daqzia.jpg" alt="daqzia" width="72" height="72"><br>
  <strong>daqzia</strong><br>
  <sub>VV：wangzhiwei-8234</sub><br>
  <sub><a href="mailto:wzwzcb@gmail.com">wzwzcb@gmail.com</a></sub><br>
  <sub><em>NullPointerException</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-xingmeng.jpg" alt="醒梦" width="72" height="72"><br>
  <strong>醒梦</strong><br>
  <sub>VV：love-is-heart-is</sub><br>
  <sub><a href="mailto:1948863412@qq.com">1948863412@qq.com</a></sub><br>
  <sub><em>Always believe that good things will happen</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-morrow.jpg" alt="morrow" width="72" height="72"><br>
  <strong>morrow</strong><br>
  <sub>VV：morrow0127</sub><br>
  <sub><a href="mailto:3201190041@qq.com">3201190041@qq.com</a></sub><br>
  <sub><em>最难不过坚持</em></sub>
</td>
</tr>
<tr>
<td align="center" valign="top" width="33%">
  <img src="assets/user-chunqiu.jpg" alt="春秋" width="72" height="72"><br>
  <strong>春秋</strong><br>
  <sub>Agent 记忆核心贡献者 · VV：A13068788886</sub><br>
  <sub><a href="mailto:558328@qq.com">558328@qq.com</a></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-song.jpg" alt="亿媒网老宋" width="72" height="72"><br>
  <strong>亿媒网老宋</strong><br>
  <sub><a href="https://em8.top">亿媒网 em8.top</a> · <a href="mailto:61731514@qq.com">61731514@qq.com</a></sub><br>
  <sub><em>全国10W+媒体、自媒体，助力企业品牌传播</em></sub>
</td>
<td align="center" valign="top" width="33%">
  <img src="assets/user-Wyatt_K.png" alt="Wyatt_K" width="72" height="72"><br>
  <strong>Wyatt_K</strong><br>
  <sub><em>千里之行，始于足下。</em></sub>
</td>
</tr>
</table>

## 3D 预演台赞助用户

感谢以下用户赞助 3D 预演台的开发。

<table>
<tr>
<td align="center" width="20%"><a href="https://github.com/TripsCoder"><img src="https://github.com/TripsCoder.png?size=128" width="64" height="64" alt="TripsCoder"><br><sub><strong>@TripsCoder</strong></sub></a></td>
<td align="center" width="20%"><a href="https://github.com/sugtex"><img src="https://github.com/sugtex.png?size=128" width="64" height="64" alt="sugtex"><br><sub><strong>@sugtex</strong></sub></a></td>
<td align="center" width="20%"><a href="https://github.com/Leyi813"><img src="https://github.com/Leyi813.png?size=128" width="64" height="64" alt="Leyi813"><br><sub><strong>@Leyi813</strong></sub></a></td>
<td align="center" width="20%"><a href="https://github.com/daoge05178"><img src="https://github.com/daoge05178.png?size=128" width="64" height="64" alt="daoge05178"><br><sub><strong>@daoge05178</strong></sub></a></td>
<td align="center" width="20%"><a href="https://github.com/AetherNo2332"><img src="https://github.com/AetherNo2332.png?size=128" width="64" height="64" alt="AetherNo2332"><br><sub><strong>@AetherNo2332</strong></sub></a></td>
</tr>
</table>

<table>
<tr>
<td align="center" width="20%"><strong>vv</strong><br><sub>2838033228@qq.com</sub></td>
<td align="center" width="20%"><strong>克里斯</strong><br><sub><a href="https://artbox.top">artbox.top</a></sub></td>
<td align="center" width="20%"><strong>宇熙</strong><br><sub>53121904@qq.com</sub></td>
<td align="center" width="20%"><strong>落羽</strong><br><sub>luoyyv66@gmail.com</sub></td>
<td align="center" width="20%"><strong>今夕何在</strong><br><sub>809321478@qq.com</sub></td>
</tr>
<tr>
<td align="center" width="20%"><strong>Alpha-M·Break</strong><br><sub>15739564793@qq.com</sub></td>
<td align="center" width="20%"><strong>不吃鸭梨</strong><br><sub>elio02519@gmail.com</sub></td>
<td align="center" width="20%"><strong>九月</strong><br><sub>563641352@qq.com</sub></td>
<td align="center" width="20%"><strong>A</strong><br><sub>jiaboxuan1205@gmail.com</sub></td>
<td align="center" width="20%"><strong>fochaobaby</strong><br><sub>leoxie.design@gmail.com</sub></td>
</tr>
</table>
