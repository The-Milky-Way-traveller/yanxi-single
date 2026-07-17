# Yanxi（盐析）— Agent-First 微模块架构

<p align="center">
  <b>一个 6MB、零依赖的 MCP 服务器，为 AI Agent 结构化管理代码项目。</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/go-1.21%2B-blue" alt="Go 版本">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="许可证">
  <img src="https://img.shields.io/badge/build-passing-brightgreen" alt="构建">
  <img src="https://img.shields.io/badge/dependencies-zero-success" alt="零依赖">
</p>

---

## 介绍

Yanxi 是一个 MCP 服务器，帮助 AI Agent 导航和维护结构化的代码项目。它接入任何兼容 MCP 的客户端（Claude Desktop、Cursor、VS Code 等），为 Agent 提供项目的结构化视图——而不是原始源码。

> **MCP（Model Context Protocol）** 是一个开放标准，让 AI 工具与外部服务器通信。可以理解为 AI 界的 USB-C 接口。Yanxi 就是这样一个 MCP 服务器。

## 为什么需要它

AI Agent 进入一个项目时，它要先读源码直到理解项目结构。5 个模块以上，那就是几千行代码。到 50 个模块时，上下文窗口完全溢出——Agent 开始在不完整的信息下做决策。这就是 **Context Cliff（上下文悬崖）**。

Yanxi 的解法：**用信息密度换信息体积。**

它做三件事：

1. **给地图** — Agent 进项目读 ~500 tokens 就能理解全貌，不用读全部源码
2. **帮检查** — Agent 写完代码，6 阶段验证自动跑
3. **记经验** — Agent 踩的坑自动写进 lessons-learned.md，下个 Agent 不重复踩

```
Agent 的任务:       决策、设计、实现
Yanxi 的任务:       结构、验证、接线、文档、记忆
```

## 怎么工作的

### 三层信息架构

```
第一层 — 项目全景（~200 tokens）
  module_discover()
  → 项目概要、模块数量、构建顺序
  → 警告：废弃模块、循环依赖、未验证模块
  → 上次 Agent 留下的教训

第二层 — 模块摘要（~300 tokens）
  → 每个模块：名称、版本、状态、语言、入口数、依赖方
  → Agent 决定下一步要看哪个模块

第三层 — 模块详情（按需加载，每个 ~300 tokens）
  module_read("auth")
  → AIexplain 知识卡片（自动生成）
  → interface.md 含类型化 Schema
  → 源码预览（前 2000 字符）
```

### AIexplain 知识层

每个模块自动生成结构化文档，Agent 读卡片不读源码：

```
# config Module
**状态**: wip | **版本**: 0.1.0 | **语言**: Go

## 用途
Package config 管理应用配置项。
**被依赖**: agent, api, mcpclient, permission, tools

## 接口
func Get() *Config {

### 入口
- **Get**: 返回全局配置实例
- **Load**: 从环境变量和配置文件初始化配置
- **Save**: 持久化配置到磁盘
- **Validate**: 校验配置合法性

## 使用示例
config.UpdateAgentModel(input)

## 被依赖
agent, api, mcpclient, permission, tools
```

入口描述从源码的 Go docstring 提取，依赖方从依赖图计算。设计意图需要 Agent 自己写——yanxi 检测到描述太泛时会警告。

### 核心机制

**微模块合约。** 每个模块附带一个机器可读的 `module.json`。描述类型化的入口 Schema、跨模块调用声明、接口 provides/uses、中间件引用和错误码——yanxi 可以验证、对比和推理的结构化数据。

**推式合约验证。** `module_validate("auth")` 自动走 6 阶段管线，把所有问题推到 Agent 面前：

- 调用指向不存在的模块或入口
- 中间件函数不存在
- 上游模块已废弃
- 下游模块的调用与新 Schema 不匹配
- Schema 变更破坏向后兼容

两者形成闭环：合约支撑验证，验证保护合约。

## 快速开始

```powershell
# 1. 编译
cd cmd\yanxi-mcp
go build -o yanxi-mcp.exe .
# → 6MB 单文件，零依赖

# 2. 配置 MCP 客户端
# 添加到 .mcp.json：
{
  "mcpServers": {
    "yanxi-single": {
      "command": "D:\\yanxi-mcp.exe",
      "args": []
    }
  }
}

# 3. 开始使用（你的 AI Agent 会调用这些）
module_discover()          → 理解项目
memory_init()              → 设置项目记忆
module_create("我的模块", "go") → 添加模块
module_validate("我的模块")     → 验证
module_wire()              → 生成路由 + HTTP 服务
```

## 18 个工具

### 进项目
| 工具 | 作用 |
|------|------|
| `module_discover` | 第一层 + 第二层。项目概要、模块摘要、警告、教训。Agent 第一个要调的工具。 |
| `module_report` | 项目健康报告：热力图、风险分、死模块检测。 |

### 模块生命周期
| 工具 | 作用 |
|------|------|
| `module_create` | 生成模块骨架（module.json + handler 模板）。支持 Go/Python/TS/JS + LLM 扩展语言。 |
| `module_read` | 第三层详情：AIexplain 卡片 + 接口文档 + 源码预览。 |
| `module_validate` | 6 阶段验证管线。yanxi 的核心。 |
| `module_wire` | 生成入口路由 + HTTP 服务（`main -http 8080`）。 |
| `module_sync` | 应用待定变更：同步入口、调用、版本。yanxi 检测→警告→Agent 确认。 |
| `module_bootstrap` | 一键创建 + 接线 + 同步。失败自动回滚。 |
| `module_adopt` | 分析外部目录用于吸收。返回 LLM 改造提示。 |
| `module_adopt_commit` | 完成吸收：写入模块、删除原目录、接线、同步。 |
| `module_deprecate` | 标记模块废弃/归档。自动写 ADR、通知依赖方。 |

### 知识与记忆
| 工具 | 作用 |
|------|------|
| `aiexplain_generate` | 增量重建 AIexplain 卡片 + 搜索索引。 |
| `memory_init` | 创建项目记忆 + 约定 + 测试模板。幂等。 |
| `memory_write` | 追加 ADR/教训/约定。自动去重。支持全局范围。 |

### 搜索与分析
| 工具 | 作用 |
|------|------|
| `module_search` | BM25/向量搜索，覆盖 AIexplain + 源码。 |
| `module_search_loose` | 搜索任意目录，无需微模块架构。 |
| `module_check_imports` | 对比声明的依赖与实际 import。 |

### 语言扩展
| 工具 | 作用 |
|------|------|
| `save_lang_template` | 保存 LLM 生成的语言模板。保存后 `module_create` 支持新语言，验证全覆盖。 |

## 验证管线（6 阶段）

| 阶段 | 检查项 | 失败模式 |
|------|--------|----------|
| ① 结构 | module.json 合法、必要字段存在、文件存在 | hard fail |
| ② 源码 | 入口函数存在、生命周期钩子存在、import 一致 | hard fail |
| ③ 跨模块 | calls 目标存在、middleware 存在、无废弃依赖、下游兼容、provides/uses 匹配、粒度 | warning/fail |
| ④ 深度分析 | import 分类、副作用、流式、约定 | warning |
| ⑤ 运行时 | 自定义测试（test_cases.json）、自动生成测试、延迟、严格模式 | warning/fail |
| ⑥ 变更检测 | Schema diff、版本建议、下游通知 | warning |

## 模块合约

```
source/modules/auth/
├── auth.py          ← handler/接口实现
└── module.json      ← 机器可读合约
```

```json
{
  "name": "auth",
  "version": "1.0.0",
  "status": "active",
  "language": "go",
  "dependencies": ["storage", "session"],
  "interface": {
    "entries": {
      "login": {
        "description": "用户登录，返回 JWT",
        "input_schema": { "type": "object", "required": ["username", "password"], ... },
        "output_schema": { "type": "object", "properties": { "token": {"type": "string"}, ... } }
      }
    },
    "provides": { "AuthService": { "methods": ["Login", "Logout", "VerifyToken"] } },
    "uses": { "storage.StorageService": { "methods": ["Save", "Load"] } },
    "calls": { "storage": {"save_session": {}} }
  }
}
```

## Yanxi 与 Agent 的分界线

| Agent 负责 | Yanxi 自动化 |
|-----------|-------------|
| 分析需求 | module.json 骨架（module_create） |
| 设计模块划分 | 路由 + HTTP 服务（module_wire） |
| 写 handler 代码 | 6 阶段验证（module_validate） |
| 写测试用例 | Schema diff + 版本建议 |
| 描述设计意图 | 知识卡片生成（aiexplain_generate） |
| 做架构决策 | 项目记忆 + 教训追踪 |

**规则**：机械的、重复的、容易忘的 → yanxi。决策、设计、业务逻辑 → Agent。

## 当前状态

Yanxi **能用但早期**。我自己在用 yanxi 开发一个 14 模块的桌面应用——验证管线每天都能抓到真实问题。

**做得好的：**
- 三层项目发现（50 模块 → ~800 tokens）
- 跨模块合约验证（calls、middleware、provides/uses、废弃检测）
- 源码到合约自动同步（入口、调用）
- 项目记忆自动去重

**还粗糙的：**
- 业务逻辑验证依赖 Agent 写测试用例
- 接口注入模式（Go struct + pubsub）展示层还需适配
- 文档和上手体验简陋
- 没有 GUI 和教程

## Yanxi 不做的事

- **业务逻辑验证** — 只验证结构，不验证正确性。Agent 写 test_cases.json。
- **骨架之外的代码生成** — 生成 module.json + 路由。Agent 写业务逻辑。
- **沙箱或进程隔离** — 仅限单机开发。
- **Dockerfile 或 CI 生成** — 部署方案因项目而异。

## 贡献

这是一个人的暑假项目。欢迎 issue、bug 报告和功能建议。
如果你试了发现哪里有问题，开一个 issue——我会看，会回。

## 许可证

MIT
