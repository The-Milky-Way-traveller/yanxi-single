# Yanxi（盐析）— Agent-First 微模块化开发工具

<p align="center">
  <b>一个 6MB、零外部依赖的 MCP 服务器，让 AI Agent 理解项目结构，不读全部源码。</b>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.21%2B-blue" alt="Go 版本">
  <img src="https://img.shields.io/badge/%E8%AE%B8%E5%8F%AF%E8%AF%81-MIT-green" alt="许可证">
  <img src="https://img.shields.io/badge/%E4%BE%9D%E8%B5%96-%E9%9B%B6-success" alt="零依赖">
  <img src="https://img.shields.io/badge/%E5%AE%9E%E7%8E%B0-Go-%2300ADD8" alt="语言">
</p>

<p align="center">
  <a href="README.md">🇬🇧 English</a>
</p>

---

## 说在最前面的话

我现在是一个高一生，没有正经的软件开发经验。这个项目是暑假 vibecoding 的一个小尝试。

由于时间和水平有限，作为一个用于软件开发的工具，这个项目目前没经历太多实战，里面有不少粗糙的地方，希望大家见谅。
目前已经实现的效果可能与下面的介绍有偏差--正是下文说的没做好上下文管理和文档记录的锅

如果你愿意花点时间看看这个基于有些天真的想法的项目，我将不胜感激。


---

##  为什么写这个

### Context Cliff（上下文悬崖）及 跨对话项目连续性

使用 vibecoding 的过程中，一旦没做好上下文管理和文档记录，经常出现一种状况：Agent 忘记了项目的意图，开始瞎猜，删除以前处理好的代码，把本来没有问题的功能摧毁。

### 解法思路

目前的工具用向量检索等方法只按相关性吧代码输入给 LLM。那能不能更进一步？

1. 在写的过程中就把相关代码放在一起，即采用**微模块架构**
2. 光拆模块对 Agent 没帮助——给每个模块配一个**概括性文件**，下次只读概括文件就能理解模块，像 skill 机制一样懒加载
3. 再进一步：每写完一个模块就**强制校验**保证可运行，下次非必要不动之前的模块

这就是 yanxi 的出发点。

---

## 什么是 Yanxi

Yanxi（盐析）是一个 MCP 服务器（6MB 单文件，零外部依赖），提供 18 个工具给 AI Agent 使用。

Yanxi-single 不做三件事：不跑流水线、不管理 Agent、不生成业务逻辑。它只做三件事：

1. **给地图** — Agent 进项目读 ~500 tokens 就能理解全貌，不用读全部源码
2. **帮检查** — Agent 写完代码自动跑 6 阶段验证，发现问题直接推到面前
3. **记经验** — Agent 踩的坑自动写进项目记忆，下次不重复踩

---

##  核心工作流

### 读取

Agent 在新对话中进入项目后，会被要求调用 yanxi，从而获取相关返回信息。调用和返回的信息分为三层：

```
第一层（约 200 tokens）:
  module_discover()
  返回：项目是干什么的、用什么语言、有多少模块、依赖关系、有哪些警告

第二层（约 300 tokens）:
  模块摘要列表 → Agent 决定要动哪个模块

第三层（按需加载，每个约 300 tokens）:
  module_read("auth") → Agent 选择了解的这个模块的完整知识卡片
  Agent 不读源码，读的是 yanxi 自动生成的知识卡片
```

*为存放这些知识卡片，我们在项目根目录建了一个文件夹- aiexplain --与源文件一一对应，专门存放知识卡片*

每一层的信息密度都远大于源码片段。Agent 读完三层能理解项目，不用碰源码。

对于一些特殊情况（非标准目录、旧项目），也允许使用 BM25 全文搜索（`module_search`）或松散搜索（`module_search_loose`）。向量搜索也是可以的，但目前没适配好。

### 微模块与模块生命周期

微模块我们定义为一个可以完整承担一项任务的模块，能够独立完成调试。其创建及连接都有相应工具以确保正确性，其契约由 yanxi + LLM 基本自动维护。编写完成后 `module_validate` 会自动多方面验证。我们还设置了模块生命周期（创建→验证→接线→弃用），致力于提升模块可用性。

### 新项目

Yanxi 自动搭建骨架。`memory_init` 一键初始化项目记忆和配置。`module_create` 一步生成模块骨架。

### 非结构文件适配

为保证 yanxi 兼容性，用 LLM 理解外部代码结构，配合 yanxi 进行外部框架快速搭建（`module_adopt`）。LLM 负责理解代码意图，yanxi 负责机械的写入、删除、接线、同步。

### 多语言适配性

目前yanxi 帮 Agent 干了不少机械活（防止 Agent 出错），但这也就意味着 yanxi 要面对不同语言，提供不同的执行模式。目前代码里只编码了 4 种语言模板（Go、Python、TypeScript、JavaScript），更多的语言会通过 LLM 生成加入模板。流程见"多语言扩展"章节。

---

## 🛠️ 18 个工具详解

### 🔍 进项目

#### `module_discover` — 项目全景地图

Agent 进项目第一个要调的工具。调用后返回三层信息：

- Level 1：项目概要（语言、模块数、构建顺序、设计意图、警告）
- Level 2：模块摘要（每个模块的名称、版本、入口数、依赖方）
- Level 3：按需加载（调用 `module_read` 获取详情）

支持懒加载模式（`lazy=true`），只返回 Level 1 + Level 2，速度更快。

返回的警告包括：废弃模块、循环依赖、未验证模块、泛描述（设计意图没写清楚）、无自定义测试用例。

#### `module_report` — 项目健康报告

聚合项目数据，输出热力图（核心模块标红）、风险分、死模块检测。改 config 前会提示"有 5 个依赖方"。

### 📦 建模块

#### `module_create` — 生成模块骨架

自动生成 `module.json` 和 handler 模板。内置支持 Go、Python、TypeScript、JavaScript。非内置语言触发 LLM 引导流程。参数：`name`（必填）、`language`（默认 python）、`description`（设计意图，建议填）。

#### `module_bootstrap` — 一键创建 + 注册 + 同步

合并 `module_create` → `module_validate` → `module_wire` → `aiexplain_generate` 为一步。失败时自动回滚。

#### `module_adopt` — 吸收外部代码

把项目里已有的非 yanxi 目录（如 `pkg/util/`）吸收进模块体系。流程：分析目录结构 → 返回 LLM 改造 prompt → LLM 写 JSON Schema → `module_adopt_commit` 写入 + 删除原目录 + 接线 + 同步。不修改原始函数体。

### ✅ 验证

#### `module_validate` — 核心功能。6 阶段自动检查

**1. 结构检查（hard fail）**：module.json 存在、JSON 合法、必要字段齐、实现文件存在。

**2. 源码检查（hard fail）**：入口函数存在（从语言模板读取正则）、生命周期钩子存在、依赖存在、import 一致。

**3. 跨模块契约检查**：calls 目标存在、middleware 存在、上游废弃检测、下游兼容性、接口契约（provides/uses）、模块粒度（>7 entry 警告）。

**4. 深度分析（warning）**：import 分类（known/local/third_party/stdlib）、side effect、streaming、自定义约定检查（conventions.json）。

**5. 运行时测试**：自定义测试（test_cases.json，不存在则 warning）、自动生成测试、多语言执行子进程（python/go/node + 模板语言）、延迟基准、严格模式。

**6. 变更检测**：Schema diff、版本建议、下游通知。

返回值：`valid`、`errors`/`warnings`、`tests`、`call_issues`、`deprecated_deps`、`middleware_issues`、`transport_issues`、`convention_issues`、`lifecycle`、`error_declarations`、`streaming_entries`、`import_scan`、`breaking_changes`。

#### `module_check_imports` — 检查依赖一致性

对比声明的依赖与实际的 import。

#### `module_sync` — 合约同步

validate 检测到源码变更后，Agent 确认要不要更新 `module.json`。Agent 也可以选择忽略 warning，手动编辑。

### 🔌 接线

#### `module_wire` — 生成路由 + HTTP 服务

生成 `source/main/main.<ext>`，含所有模块的 import、路由分发、HTTP 服务器（`main -http 8080` 启动）。CLI 模式和 HTTP 模式双入口。未验证的模块阻塞生成。

### 📝 文档

#### `aiexplain_generate` — 生成 AIexplain 知识卡片

增量模式，只更新有变更的模块。为每个模块生成 `<name>.md`（模块概述）和 `interface.md`（接口参考）。卡片内容来源：`module.json` 的 description、源码 docstring、依赖图、错误码。

#### `memory_init` — 初始化项目记忆

幂等操作。创建 `architecture-decisions.md`、`lessons-learned.md`、`conventions.md`、`conventions.json`（结构化约定，validate 自动检查）、`test_cases.json`（自定义测试模板）、`.yanxi/project.json`。

#### `memory_write` — 写入项目记忆

ADR/教训/约定。自动去重。支持 `scope="project"`（本地）和 `scope="global"`（跨项目，写入 `~/.yanxi/memory/`）。

### 🗑️ 弃用

#### `module_deprecate` — 模块弃用

改状态为 deprecated/archived，自动写 ADR、通知依赖方。

### 🔎 搜索

#### `module_search` — 全文搜索

BM25 搜索，覆盖 AIexplain + 源码。编译加 `-tags vector` 启用向量搜索。

#### `module_search_loose` — 松散搜索

搜索任意目录，不需要 yanxi 架构。用于旧项目或非标准目录。

### 🌐 多语言扩展

#### `save_lang_template` — 保存 LLM 生成的语言模板

内置支持 Go、Python、TypeScript、JavaScript。对其他语言：

1. `module_create("auth", "rust")` → yanxi 没有内置模板 → 返回 LLM prompt
2. Agent 把 prompt 发给 LLM → prompt 含 Go 和 Python 的完整 JSON 样例 → LLM 返回 Rust 模板 JSON（含 `entry_regex`、`import_extract_regex`、`test_runtime` 等字段）
3. `save_lang_template("rust", <JSON>)` → yanxi 存入 `.yanxi/lang-templates/rust.json`
4. 之后：`module_create` 生成骨架 ✅、`module_validate` entry 检查和 import 分类 ✅、测试执行 ✅（新增模板回退）、`module_wire` 生成路由 ✅

---

## 📐 模块合约

```
source/modules/auth/
├── auth.py          ← handler/接口实现
└── module.json      ← 机器可读合约
```

| 字段 | 用途 | 谁填 |
|------|------|------|
| `name` | 模块标识 | module_create 自动 |
| `version` | 语义版本号 | Agent 手动 / validate 建议 |
| `status` | wip → active → deprecated → archived | Agent / deprecate 工具 |
| `language` | 编程语言 | module_create 自动 |
| `dependencies` | 依赖的 yanxi 模块 | Agent 填 / validate 检查 |
| `interface.description` | **设计意图**——模块为什么存在 | **Agent 必须填**，validate 检查 |
| `interface.entries` | 可调用的入口函数 | validate 自动同步 |
| `interface.provides` | 模块提供的接口 | Agent 填 |
| `interface.uses` | 模块消费的接口 | Agent 填 / validate 检查 |
| `interface.calls` | 跨模块函数调用 | validate 自动推断 |
| `errors` | 模块可能返回的错误码 | Agent 填 |

---

## 🔄 Agent 工作流

```
module_discover()          → 理解项目（~500 tokens）
module_read("auth")        → 理解目标模块
编辑 handler 代码
写 test_cases.json         → validate 会警告如果缺失
写 module.json description → validate 会警告如果太泛
module_validate("auth")    → 6 阶段验证
module_sync("auth")        → 确认变更（如果有新入口/调用）
module_wire()              → 生成路由 + HTTP
aiexplain_generate()       → 同步知识卡片
```

---

## ⚠️ 当前状态

项目可用性还在评估。目前我自己有用 yanxi 开发一个桌面应用（yanxi-desktop）。

### 能做好的

- 三层项目发现（50 模块 ~800 tokens）
- 跨模块合约验证（calls、middleware、provides/uses、废弃检测）
- 源码 → 合约自动同步（入口、调用）
- 项目记忆自动去重
- provides/uses 接口声明
- 多语言模板框架

### 还粗糙的

- **本身为纯 vibecoding 产物，项目极为早期，且可靠性未完整评估**
- 业务逻辑验证要靠 AI 写测试用例。yanxi 只提供框架（test_cases.json），测试内容得 AI 自己写。auto-sync 的 entry 描述是占位符，需要 AI 主动填
- 模块通信模型偏向函数路由。yanxi 默认假设模块之间调 `handler(input: dict) → dict`。但很多 Go 项目用接口注入 + pubsub，这个模式支持得不够好。`provides/uses` 声明系统已经加了，但展示层和生成层还要适配
- 小项目（3 模块以下）用起来不值。直接写代码比配 yanxi 快
- 文档和上手体验还很粗糙。没有 tutorial、没有 GUI，全靠 MCP 工具调用
- **对严肃的软件开发工作流适配性存疑**

### 接下来

暑假时间实在有限，这个项目我可能会先放一放，但不是放弃。

如果有人愿意试用、提 issue、或者告诉我哪里做得不对，我会认真对待并万分感激。

GitHub: [https://github.com/The-Milky-Way-traveller/yanxi-single](https://github.com/The-Milky-Way-traveller/yanxi-single)

---

## 🚀 快速开始

```powershell
cd cmd\yanxi-mcp
go build -o yanxi-mcp.exe .
# → 6MB 单文件，零依赖
```

---

## 📄 项目结构

```
<project>/
├── .yanxi/                    ← 工具状态（自动管理）
│   ├── project.json           ← 项目配置
│   ├── discover_cache.json
│   ├── schema_cache/
│   ├── validation_state.json
│   ├── search_index.json
│   └── lang-templates/        ← LLM 生成的语言模板
├── source/
│   ├── main/main.{py|ts|go}   ← 接线入口
│   └── modules/<name>/
│       ├── <name>.{py|ts|go}  ← handler
│       └── module.json        ← 合约
├── AIexplain/                 ← 知识层（自动生成）
├── project-memory/            ← 项目记忆
│   ├── conventions.json       ← 结构化约定（validate 自动检查）
│   └── test_cases.json        ← 自定义测试模板
└── .mcp.json
```

---

## 📜 许可证

MIT
