# Yanxi — 现状评估与发展路线

> yanxi-single v1.1.0 — 2026-07-17

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
**修复优先级**：高。已在 v1.1.0 中通过 `module_adopt` + 建议模式解决。
**当前状态**：✅ 已缓解。module_adopt + LLM prompt + module_sync 确认模式。

### 2.2 测试执行脆弱

validate 启动子进程跑测试（`python -c` / `node -e` / `go run`），依赖：

- 语言运行时在 PATH 上
- 临时文件系统可写
- 生成的测试代码无 bug

**严重程度**：中等。测试是 validate 的附属功能，核心结构验证不受影响。
**修复优先级**：中。
**当前状态**：✅ 已修复。base64 编码替代字符串内联解决转义 bug。运行时不可用降级为 warning。

### 2.3 没有沙箱

多个模块在同一个进程空间跑。一个模块 panic 拖垮整个进程。无资源限制（内存/CPU），无网络隔离。

这是 yanxi 的设计决定——"模块是同一个进程里的函数"。

**严重程度**：对单机开发没问题，对生产部署是硬伤。
**修复优先级**：低。生产隔离是 Agent 的事（Agent 生成 Dockerfile/k8s）。
**当前状态**：❌ 明确不做。已标记在 ROADMAP。

### 2.4 calls 与实际架构有断层

当前 calls 模型是"模块.入口"。对于 Go struct 接口（如 `session.Service`）无法表达

**严重程度**：对于 handler 路由风格项目够用，对于 Go struct/pubsub 风格项目半对半不对。
**修复优先级**：中。
**当前状态**：✅ 已修复。v1.1.0 新增 `provides/uses` 接口声明系统，validate 检查接口契约匹配。

### 2.5 没有项目管理视角

yanxi 是模块级工具。它知道每个模块的依赖，但不知道：

**严重程度**：低。不影响当前功能，但长期维护需要。
**修复优先级**：中。
**当前状态**：✅ 已解决。v1.1.0 新增 `module_report` 工具（热力图 + 风险分 + 核心模块 + 死模块检测）。

### 2.6 多语言一致性有代价

内置四语言（Go/Python/TS/JS）全覆盖。LLM 模板生成的语言（Rust/C#/Kotlin...）在 validate 里只能做基础结构检查

**严重程度**：对主力开发语言足够。边缘语言需要用户自己验证模板质量。
**修复优先级**：中。
**当前状态**：✅ 已修复。v1.1.0 中 ScanImports 从 LangTemplate 读取 import_regex，validate 全面走模板。

---

## 三、解决方案

### 3.1 架构约束 → describe-first adopt

**方案**：不要让 LLM 改代码。让 LLM 只做一件事：为每个导出函数写 JSON Schema 描述。yanxi 根据描述自动生成 handler 层。

**当前状态**：✅ 已通过 `module_adopt` + 建议模式实现。`module_sync` 工具应用变更前需 Agent 确认。

### 3.2 测试脆弱 → 修复 + 降级

**当前状态**：✅ 已修复。base64 编码替代字符串内联。运行时不可用降级为 warning。

### 3.3 沙箱 → 不修，明确边界

**yanxi 是开发时的验证工具，不是生产时的运行时。** 生产隔离由 Agent 负责。Roadmap 上标记为"不会做"。

### 3.4 calls 断层 → 加 provides/uses 声明

**当前状态**：✅ 已实现。v1.1.0 新增 provides/uses 接口声明系统。validate 检查接口契约匹配。

### 3.5 项目管理 → module_report 工具

**当前状态**：✅ 已实现。v1.1.0 新增 `module_report` 工具，输出热力图、风险分、核心模块、死模块检测。

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

### 短期（v1.1.0 已完成）

| 修复 | 状态 |
|------|------|
| 测试修复 + 降级 | ✅ base64 编码 + 运行时 warning |
| provides/uses 接口声明 | ✅ validate 检查接口契约 |
| module_report 热力图 | ✅ 风险分 + 核心模块 + 死模块 |
| validate 读 test_runtime | ✅ import 扫描走模板 |
| module_wire HTTP 服务 | ✅ 生成 net/http 双模式入口 |
| auto-sync 建议模式 | ✅ module_sync 工具，Agent 确认 |

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
