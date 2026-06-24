# 半衰期（half_life）设计分析

## 背景

当前 [`TripTraitsFeature`](internal/remote/agent/toolimp/trip_traits.go:35) 结构体包含字段：

```go
type TripTraitsFeature struct {
    CategoryID   int            `json:"category_id"`
    CategoryName string         `json:"category_name"`
    FeatureText  string         `json:"feature_text"`
    Keywords     []TraitKeyword `json:"keywords"`
    Confidence   int            `json:"confidence"`
}
```

之前的设计文档 [`个人特征设计.md`](doc/plans/个人特征设计.md:15) 已规划了 `half_life: short/medium/long` 字段。现有问题是：**是否要在提取层（trip_traits 输出）加入半衰期？**

---

## 核心问题：提取层 vs 存储层

### 现有分层

```
LLM 特征提取（trip_traits）  →  SSE 推送结果  →  消费/存储
   ↑ 提取层                          ↑ 传输层        ↑ 存储层
```

半衰期应该放在哪一层？

### 选项 A：仅存储层根据 category 计算（不修改提取层）

- 优点：无需修改 LLM 提示词、Go 结构体、i18n，改动量小
- 缺点：粒度粗，同一 category 的不同特征无法区分时效

### 选项 B：提取层和存储层都加入（推荐）

- 优点：LLM 具有语义理解能力，能判断细粒度时效差异
- 缺点：需要修改多处代码

---

## 通过 Category 推断的粒度分析

| Category | 默认半衰期 | 同一 Category 内是否有差异？ | 示例 |
|----------|-----------|---------------------------|------|
| 1 人口学特征 | long | 有时有 | "25岁"→long, "月薪八千"→medium |
| 2 外部客观事实 | long | 有时有 | "养一条狗"→long, "手机是iPhone15"→medium(换机) |
| 3 文化修为 | long | 基本稳定 | "出版过诗集"→long/permanent |
| 4 兴趣爱好 | medium-long | 有差异 | "爱打篮球"→long, "最近迷上健身"→medium |
| 5 能力技能 | long | 基本稳定 | "会Python"→long |
| 6 偏好/癖好 | long | 基本稳定 | "讨厌香菜"→long |
| 7 行为习惯 | long | 有差异 | "十年烟民"→long, "晚睡晚起"→medium(可能改) |
| 8 健康与疾病 | medium-long | 有差异 | "花粉过敏"→long, "失眠"→medium(可治愈) |
| **9 近况和状态** | **short** | **有差异** | "刚刚失恋"→**short**, "正在准备考公"→**medium** |
| 10 人格/性格 | long | 基本稳定 | "责任心强"→long |
| 11 价值观与信仰 | long | 基本稳定 | "环保主义"→long |
| 12 社交关系 | medium-long | 有差异 | "两个孩子的爸"→long, "老板很抠门"→medium(换工作) |
| 13 人生事件 | permanent | 基本稳定 | "参加过抗洪"→permanent |
| 14 目标与计划 | medium | 有差异 | "想减肥"→medium, "计划下周去日本"→short |

结论：**仅靠 category 推断半衰期，在 Category 9 内部确实存在粒度不足的问题。**

---

## 你的问题："Category 9 本身就是 short 时效"

你的观察是对的：Category 9 的定义——"临时、可变化的状态（时效较短）"——确实本质上就是 short 半衰期。

但问题是：**Category 9 内部仍然存在细节差异：**

- "刚刚失恋了" → 可能只需 short（几周）
- "正在准备考公" → medium（几个月，甚至一年）
- "最近工作压力大" → short（几周）

同理，**其他 Category 也有跨类别差异：**

- Category 1 "25岁" → long（一年才变一次），但"月薪八千" → medium（可能跳槽涨薪）
- Category 13 "抗洪经历" → permanent（永不过期），但 Category 13 "大学支教过一年" → long（已完成事件不过期）

所以，**category 无法完全替代 half_life**。

---

## 建议方案：在提取层加入 half_life

### 修改范围

#### 1. Go 结构体 [`TripTraitsFeature`](internal/remote/agent/toolimp/trip_traits.go:35)

```go
type TripTraitsFeature struct {
    CategoryID   int            `json:"category_id"`
    CategoryName string         `json:"category_name"`
    FeatureText  string         `json:"feature_text"`
    Keywords     []TraitKeyword `json:"keywords"`
    Confidence   int            `json:"confidence"`
    HalfLife     string         `json:"half_life"`  // 新增：short/medium/long/permanent
}
```

#### 2. JSON Schema（Strict Mode）

在 [`tripTraitsToolDefinition`](internal/remote/agent/toolimp/trip_traits.go:80) 的 properties 中加入：

```go
"half_life": map[string]any{
    "type": "string",
    "enum": []string{"short", "medium", "long", "permanent"},
    "description": i18n.Tools.TL(lang, TripTraitsToolName, "param_half_life_desc"),
},
```

并在 required 数组中加入 `"half_life"`。

#### 3. i18n 本地化文件

- [`lang/remote/zh-CN/tools/trip_traits.toml`](lang/remote/zh-CN/tools/trip_traits.toml)：
  - 新增 `[param_half_life_desc]` 描述字段含义
- [`lang/remote/en/tools/trip_traits.toml`](lang/remote/en/tools/trip_traits.toml)：
  - 同上（英文版）

#### 4. 系统提示词 [`lang/remote/zh-CN/system_prompt.toml`](lang/remote/zh-CN/system_prompt.toml)

在特征分类表之后，输出格式说明中，加入 half_life 字段的判定规则：

> **半衰期（half_life）判定规则**：
> `half_life` 表示该特征的有效期长短，取值为 `short/medium/long/permanent`。
> 它不是基于 category_id 机械映射的，而是基于你对**该特征实际性质**的判断：
> - **short**：预计几周内可能变化。通常适用于当前临时状态、情绪、短期目标
> - **medium**：预计几个月到一年可能变化。通常适用于阶段性状态、可调整的习惯
> - **long**：预计一年以上稳定。通常适用于稳定偏好、技能、人格特质、长期习惯
> - **permanent**：已完成的客观事件，不会改变。如历史经历、文化修为成就

具体判定参考：

| 取值 | 有效期 | 典型场景 | 示例 |
|------|--------|----------|------|
| short | 天~周 | 临时状态、近期情绪、短期计划 | "刚刚失恋"、"最近工作压力大" |
| medium | 月~年 | 阶段性项目、备考、可调整的习惯 | "正在准备考公"、"晚睡晚起" |
| long | 年+ | 稳定偏好、技能、人格、长期习惯 | "讨厌香菜"、"会Python"、"责任心强" |
| permanent | 永不 | 已完成的客观历史事件 | "参加过抗洪"、"出版过诗集" |

**重要提醒**：half_life 不是 category 的简单映射，必须基于特征本身的实际性质判断。例如：
- Category 9 "近况和状态"不总是 short——"正在准备考公"更接近 medium
- Category 1 "人口学特征"不总是 long——"月薪八千"可能 medium（换工作可能变化）
- Category 13 "人生事件"通常 permanent，但若描述的是"最近发生的事件"则可能是 short

#### 5. 解析器（不影响）

[`extractStringFieldDirect`](internal/remote/agent/toolimp/trip_traits.go:351) 和 [`decodeSingleFeature`](internal/remote/agent/toolimp/trip_traits.go:258) 的 fallback 逻辑中，`half_life` 作为枚举字符串字段，不需要特殊处理——直接用现有的 `extractStringFieldDirect` 即可。

但需要在 [`decodeSingleFeature`](internal/remote/agent/toolimp/trip_traits.go:258) 的 fallback 路径中加上 half_life 的提取：

```go
f.HalfLife = extractStringFieldDirect(data, "half_life")
```

#### 6. 前端消费

在 [`cmd/remote-server/main.go:401`](cmd/remote-server/main.go:401) 推送 `tool_result` 和最终 `done` 事件时，`half_life` 会自动随 JSON 输出。前端可根据 half_life 对特征进行不同样色的标记或排序。

---

### 不需要改动的部分

- SQLite 表结构（`chat_sessions`、`chat_messages`）—— traits 目前以 JSON 形式推送，未被持久化到独立表中
- 管道逻辑 [`pipeline.go`](internal/remote/agent/pipeline.go) —— 无需改动
- SSE 事件处理逻辑 [`main.go:400`](cmd/remote-server/main.go:400) —— `half_life` 作为 TripTraitsFeature 的字段，会自动序列化

---

## 后续待办（未来）

如果未来将 traits 持久化到独立数据库表，[`half_life`](doc/plans/个人特征设计.md:15) 将用于以下场景：

1. **特征老化过滤**：定期清除或降低 short half-life 已过期的特征优先级
2. **特征合并**：同 category_id 但不同 half_life 的特征合并策略
3. **检索权重**：long/permanent 特征权重高于 short 特征

---

## 总结：加入 half_life 的收益 vs 成本

| 维度 | 不加（仅靠 category 推断） | 加入（提取层输出） |
|------|--------------------------|-------------------|
| 粒度 | category 级别粗粒度 | 特征级别细粒度 |
| LLM 理解力 | 未利用 | 充分利用 LLM 语义理解 |
| 改动量 | 无 | 中等（5 个文件） |
| Category 9 特殊处理 | 需硬编码规则 | 天然解决 |
| 跨 category 差异 | 无法处理 | 可处理（如月薪八千=medium） |
| 未来扩展性 | 差 | 好（直接支持老化、权重） |

### 最终建议：**加入 half_life**
