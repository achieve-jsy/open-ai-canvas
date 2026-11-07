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
