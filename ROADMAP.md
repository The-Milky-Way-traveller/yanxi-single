# Yanxi — 现状评估与发展路线

> yanxi-single v1.0.0 — 2026-07-17

---

## 一、当前优势

### 1. 信息密度碾压

128K 上下文窗口中，14 模块的项目 `module_discover` 只用 ~500 tokens 给出全貌。不读 yanxi 的话，Agent 需要把 14 个包的源码全部塞进窗口——3 万 token 起步。模块越多差距越大。

```
传统:       n 模块 × k 行源码 = O(n·k) tokens 理解项目
Yanxi:      n × 20 (摘要) + T (按需详情) = O(20n + T) tokens

n=50 模块:
  传统:     ~100,000 tokens（窗口满，细节丢失）
  Yanxi:   ~1,200 tokens + 500/模块 按需
```

### 2. 验证深度远超"语法检查"

不是 lint，不是编译。yanxi 的 validate 覆盖：

| 层 | 检查项 | 同类工具能做到？ |
|----|--------|----------------|
| 结构 | 合约完整性（name/version/status/interface） | ❌ 不知道合约长什么样 |
| 源码 | entry 函数存在性 | ⚠️ LSP 能检查符号存在性 |
| 跨模块 | calls 目标存在、middleware 存在、废弃依赖、下游兼容性 | ❌ 不理解跨模块契约 |
| 深度 | import 五类分类、side effect、streaming | ❌ 不懂你的架构约定 |
| 运行时 | 自动测试生成 + 执行 + 延迟基准 | ⚠️ 要你自己写测试 |
| 变更 | schema diff + version bump + downstream 通知 | ❌ 不追踪接口历史 |

### 3. 错误经验不丢失

Agent A 踩坑 → 自动写 `lessons-learned.md` → Agent B 进项目第一屏就看见。

对长期维护的项目价值巨大——人的团队有 wiki 和 code review，Agent 团队什么都没有。yanxi 给了它。

### 4. 机械劳动全自动化

| 操作 | 以前 | 现在 |
|------|------|------|
| 加了新 handler 函数 | 手动同步 module.json | validate 自动写入 |
| 改了接口参数 | 手动测试 | schema diff + strict 模式自动报 |
| 调了另一个模块 | 手动声明 calls | validate 自动推断 |
| 删了旧入口 | 留下死声明 | validate 报 entry 找不到 |
| 上游改了接口 | 靠自己发现 | downstream 兼容性扫描 |

---

## 二、当前劣势

### 2.1 架构假设有强约束

```
模块必须在: source/modules/<name>/
必须有:    module.json（name、version、interface、dependencies）
接口必须:  handler(input: dict) → dict
```

对已存在的项目迁移成本不低。`module_adopt` 能帮一部分，但 LLM 生成的 handler 层质量取决于 LLM 本身。

**严重程度**：对新项目零成本。对老项目需要一次性投入（分析 + LLM 改造）。
**修复优先级**：高。已经在解决中。

### 2.2 测试执行脆弱

validate 启动子进程跑测试（`python -c` / `node -e` / `go run`），依赖：

- 语言运行时在 PATH 上
- 临时文件系统可写
- 生成的测试代码无 bug

当前 Go 测试有 `illegal character U+0073 's'` 转义 bug。语言运行时不可用时 validate 直接跳 fail。

**严重程度**：中等。测试是 validate 的附属功能，核心结构验证不受影响。
**修复优先级**：中。

### 2.3 没有沙箱

多个模块在同一个进程空间跑。一个模块 panic 拖垮整个进程。无资源限制（内存/CPU），无网络隔离。

这是 yanxi 的设计决定——"模块是同一个进程里的函数"。

**严重程度**：对单机开发没问题，对生产部署是硬伤。
**修复优先级**：低。生产隔离是 Agent 的事（Agent 生成 Dockerfile/k8s）。

### 2.4 calls 与实际架构有断层

当前 calls 模型是"模块.入口"。对于 Go struct 接口（如 `session.Service`）无法表达——调用方拿到的不是模块，是 interface 实例。

```json
// 当前能表达的：
"config": { "handler": {} }

// 当前不能表达的：
"session.SessionService": { "methods": ["Get", "Update"] }
```

**严重程度**：对于 handler 路由风格项目够用，对于 Go struct/pubsub 风格项目半对半不对。
**修复优先级**：中。需要设计接口描述规范。

### 2.5 没有项目管理视角

yanxi 是模块级工具。它知道每个模块的依赖，但不知道：

- 哪个是核心模块？（被最多依赖 = 核心？）
- 哪个风险最高？（变更频率 × 依赖方数量）
- 哪些是死模块？（0 依赖方 + 0 变更）

**严重程度**：低。不影响当前功能，但长期维护需要。
**修复优先级**：中。

### 2.6 多语言一致性有代价

内置四语言（Go/Python/TS/JS）全覆盖。LLM 模板生成的语言（Rust/C#/Kotlin...）在 validate 里只能做基础结构检查：

```
✓ module_create         → 生成骨架
✓ module_wire           → 生成路由
✓ entry 存在性检查      → 走模板正则
✗ 测试执行              → 跳过（default: continue）
✗ import 分类扫描       → 回退到通用正则
```

**严重程度**：对主力开发语言足够。边缘语言需要用户自己验证模板质量。
**修复优先级**：中。Template 的 validate 字段已有，缺 validate 全面读取。

---

## 三、解决方案

### 3.1 架构约束 → describe-first adopt

**方案**：不要让 LLM 改代码。让 LLM 只做一件事：为每个导出函数写 JSON Schema 描述。yanxi 根据描述自动生成 handler 层。

```
# 当前（LLM 改源码，质量不可控）:
module_adopt("pkg/util")
  → 返回 "请把这段代码改成加 handler 的形式"
  → LLM 改 → 可能改出 bug

# 改进（LLM 只描述，yanxi 生成）:
module_adopt("pkg/util")
  → 扫描导出函数
  → LLM 为每个函数写 JSON Schema
  → yanxi 根据 schema 生成 handler 转发代码
  → 纯机械，不碰原始函数体
```

### 3.2 测试脆弱 → 修复 + 降级

```go
// 修复 Go 转义 bug:
// 用 base64 编码输入代替字符串字面量
inputJSON := base64.StdEncoding.EncodeToString(jsonInput)
// 生成代码中解码

// 运行时不可用 → 降级为 warning 不是 invalid:
if runtimeNotFound {
    r.Warnings = append(r.Warnings, "skip: python not found")
    return  // 不设置 r.Valid = false
}
```

### 3.3 沙箱 → 不修，明确边界

**yanxi 是开发时的验证工具，不是生产时的运行时。** 生产隔离由 Agent 负责。Roadmap 上标记为"不会做"。

### 3.4 calls 断层 → 加 provides/uses 声明

```json
// 提供方声明接口:
{
  "name": "session",
  "interface": {
    "provides": {
      "SessionService": {
        "methods": ["Get", "List", "Create", "Update", "Delete"]
      }
    }
  }
}

// 消费方声明使用:
{
  "name": "agent",
  "interface": {
    "uses": {
      "session.SessionService": {
        "methods": ["Get", "Update"]
      }
    }
  }
}
```

validate 检查：提供方的接口方法是否存在 + 消费方只调了声明的方法。

### 3.5 项目管理 → module_report 工具

聚合现有数据，新增一个工具：

```
module_report()
  → 热力图:
      config 被 4 模块依赖 + 上周改 3 次 → ⚠ 核心/高频变更
      diff   0 依赖方 + 3 年没改 → 🧊 可能是死模块

  → 风险分:
      未验证: 2/14
      废弃依赖: 0
      测试通过率: 85%

  → 建议:
      改 config 前通知 agent, api, mcpclient, tools
```

纯数据聚合，不需要新基础设施。

### 3.6 多语言 → validate 读取模板 test_runtime

```go
// 当前:
switch lang {
case "python": runPyTest(...)
case "go":     runGoTest(...)
default:       continue  // 跳过
}

// 改为:
if tmplErr == nil && langTmpl.Validate.TestRuntime != "" {
    cmd := replacePlaceholders(langTmpl.Validate.TestRuntime, tmpFile)
    runCmd(cmd)
}
```

LLM 生成的 Rust 模板包含 `test_runtime` → validate 直接能用。不需要 yanxi 内建 Rust 支持。

---

## 四、长期路线

### 短期（现在 ~ 下个月）：补缺口

| 修复 | 工作量 |
|------|--------|
| describe-first adopt | 2-3 天 |
| 测试修复 + 降级 | 半天 |
| provides/uses 接口声明 | 2-3 天 |
| module_report 热力图 | 1-2 天 |
| validate 读 test_runtime | 半天 |

### 中期（1-3 个月）：跨项目

```
跨项目记忆:
  Agent 在项目 A 学的经验 → 自动出现在项目 B 的 lessons-learned
  不是 RAG 检索，是结构化经验推送

项目间依赖图:
  项目 A 依赖 B 的模块 → yanxi 跨项目检查接口兼容性
```

这些可数据结构扩展实现：`lessons-learned.md` 本来就有格式，加一个 `project` 字段即可。跨项目导入检查就是多一个 `source` 目录。

### 远期（6 个月+）：多 Agent 协作

```
两 Agent 同时改一个项目 → yanxi 做变更协调

  Agent A 改了 config 的 GetModel 签名
  → yanxi 锁住 agent 模块
  → 等 Agent B 确认适配后再解锁

不是 git merge（代码级），是契约级协调。
```

yanxi 现有的 `validation_state` + `schema_cache` + `calls` 声明已经是协议的基础。扩展多 Agent 是数据结构的自然延伸。

---

## 五、不会做的

| 不做 | 原因 |
|------|------|
| HTTP 服务器代码生成 | Agent 5 秒生成 FastAPI/Gin，yanxi 替做是过度去 agent |
| 模块沙箱/进程隔离 | 单机开发不需要，生产部署是 Agent 的事 |
| 事件总线运行时 | Agent 需要时才加，yanxi 不预设运行方案 |
| 自定义测试框架 | Agent 自己写 pytest/go test，yanxi 只负责跑已有的 |
| Dockerfile/CI 生成 | 部署方案每个项目不同，Agent 决策 |

---

## 六、定位一句话

> yanxi 对"一个人用 AI 迭代一个模块化的项目"是利器——信息密度、自动化验证、经验持久化都是独有优势。
>
> 对"多人生产环境部署一个单体"是半成品——缺项目级视图、测试执行脆弱、calls 抽象和实际代码有断层。
>
> 但这两个极端中间的 80% 场景，yanxi 已经是"唯一的选择"——没有第二个工具能做跨模块契约验证和自动经验捕获。
