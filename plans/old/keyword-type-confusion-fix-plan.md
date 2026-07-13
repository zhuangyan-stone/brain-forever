# 关键词类型混淆问题修复方案

## 问题分析

### 问题一：关键词 `type` 错误使用了个人特征 `category_id`

**症状**：AI 在提取关键词时，将个人特征的 14 分类编号（`category_id` 1-14）错误地填入关键词的 `type` 字段（应为 1-6）。

**示例**：
```json
{
  "category_id": 9,
  "category_name": "近况和状态",
  "feature_text": "工作压力",
  "keywords": [
    { "type": 9, "word": "工作压力" }  // ❌ type=9 不存在；关键词与特征文本完全重叠
  ]
}
```

**根因分析**：在 [`lang/remote/zh-CN/system_prompt.toml`](lang/remote/zh-CN/system_prompt.toml) 中：

1. **特征分类表**（第25-40行）定义了一个 14 类的编号系统（1-14），使用 `编号` 作为列名
2. **关键词 type 定义**（第64-70行）定义了另外一套 6 类编号系统（1-6），使用 `type` 作为列名
3. AI 容易将两个编号系统混淆，因为在输出 JSON 中，`category_id`（14类）和 `keywords[].type`（6类）都用数字表示，且位置接近

**关键观察**：AI 有时会直接复用 `category_id` 的值作为 `keywords[].type` 的值，因为：
- 两者都是整数编号
- 都在同一个 feature 对象中
- AI 没有形成清晰的"两套独立编号系统"的心智模型

### 问题二：关键词与特征文本完全重叠

**症状**：`keywords[].word` 直接复制了 `feature_text` 的内容，而不是提炼出原子化的关键词元素。

**根因**：提示词中没有明确要求关键词应该比特征文本更原子化、更具体。

### 问题三：抽象名词无法归类

以"工作压力"为例，如果将其拆解为关键词：

| 候选词 | 词性 | type 1(时间) | type 2(地点) | type 3(人) | type 4(物品) | type 5(关系) | type 6(行为) |
|--------|------|:-:|:-:|:-:|:-:|:-:|:-:|
| 工作 | 名词 | ✗ | ✗ | ✗ | ? | ✗ | △(可视为行为) |
| 压力 | 抽象名词 | ✗ | ✗ | ✗ | ✗(严格物品) | ✗ | ✗ |

- "工作"可勉强归为 type 6（行为），但语义是"职业/工作压力"中的名词性用法
- "压力"是抽象心理状态，完全没有现有分类可匹配
- 现有 type 4 定义为"非人类的、**具体的事物**（物品、动物、植物、用户自身属性等）"，明确排除了抽象名词

---

## 修复方案

### 方案 A：将 type 4 从"物品"扩展为"事物"（推荐）

**核心变更**：将关键词 type 4 的名称从 **"物品"** 改为 **"事物"**，定义从"具体事物"扩展为"具象或抽象实体、概念、领域"。

#### 理由

1. **向后兼容**：现有的 type 4 用法（诗集、篮球、海尔冰箱、历史、医学等）全部仍然有效
2. **覆盖抽象名词**：压力、工作（作为领域/概念）、梦想、健康、财富等抽象名词可纳入
3. **与现有"抽象化原则"一致**：第68行已要求将"中医→医学"、"唐诗→文学"等抽象化处理，说明 type 4 本就部分承载了抽象概念
4. **语义合理**："事物"天然包含"物品"（具象事物）+ "概念"（抽象事物）
5. **单一改动点**：只需修改系统提示词和工具描述中的 type 4 定义，无需改 Go 代码的 struct 定义或 JSON schema

#### 不选用方案 B（新增 type 7"抽象概念"）的理由

1. 破坏 1-6 的编号体系，需要修改 Go 代码注释和 tool 参数描述
2. AI 又多了一个选择项，增加了分类决策负担
3. 对同一个特征可能同时出现 type 4（物品）和 type 7（抽象概念），分类边界模糊
4. "工作压力"中的"工作"可以同时被理解为"事物（职业领域）"和"行为（工作动作）"，反而制造了新困惑

### 关于"工作压力"的关键词提取建议

```json
{
  "category_id": 9,
  "category_name": "近况和状态",
  "feature_text": "最近工作压力大",
  "confidence": 7,
  "keywords": [
    { "type": 4, "word": "工作" },
    { "type": 4, "word": "压力" }
  ]
}
```

若用户明确提及时间（如"最近"），可额外提取：
```json
{ "type": 1, "word": "最近" }
```

---

## 具体修改点

### 修改 1：`lang/remote/zh-CN/system_prompt.toml` — 关键词 type 4 定义

**位置**：第64-70行（关键词提取规则部分）

**改动**：
1. type 4 名称从 `物品` → `事物`
2. 定义从 "非人类的、具体的事物（物品、动物、植物、用户自身属性等）" → 包含抽象名词
3. 添加强调说明：判断关键词类型时，**必须独立于特征 `category_id`**，`keywords[].type` 始终是 1-6

**详细修改**：

```
- **type 4: 事物**：特征中提及的**具象或抽象实体**，包括：
  - **具象事物**：物品（手机、冰箱、诗集、篮球）、动物（猫、狗）、植物（花草树木）、用户自身属性（体重、身高）
  - **抽象概念**：领域/学科（工作、医学、编程、教育）、状态/情绪（压力、焦虑、快乐）、品质/属性（信用、财富、健康）
  
  **重要**：凡是指代具体人物（真实的人名、历史人物名、角色名等）的，应归为 type 3（人），而非 type 4（事物）。
  
  事物关键词**必须为名词或名词性短语**，不能是动词或动宾词组。
  
  示例：
  - "家里的海尔冰箱用了15年" → `{"type":4, "word":"海尔冰箱"}`
  - "爱打篮球" → `{"type":4, "word":"篮球"}`
  - "工作压力大" → `{"type":4, "word":"工作"}`, `{"type":4, "word":"压力"}`
  - "懂点中医知识" → `{"type":4, "word":"中医"}`
  - "希望将来能更自由" → `{"type":4, "word":"自由"}`
  
  **不记录**公共的非特定事物，如用户说 "今天天气真好"，不要提取"天气"。再如"我好想给你整个宇宙"，不要记录"宇宙"。
```

### 修改 2：`lang/remote/zh-CN/system_prompt.toml` — 添加防混淆警告

在**关键词提取规则**段落之前或之后，新增一段**⚠️ 重要：关键词 type 与特征 category_id 的区别**：

```
**⚠️ 重要：关键词 type 与特征 category_id 的区别**：

特征（feature）的 `category_id` 使用的是 **14 类个人特征分类**（对应上文的"特征分类表"），而**关键词（keyword）的 `type` 使用的是独立的 6 类关键词分类**（定义如下）。

**关键词 type 的取值范围始终是 1~6，绝不能使用 7~14。**
**关键词 type 绝不等于特征 category_id！** 两者是完全独立的编号体系。

例如：
- 特征 `category_id=9`（近况和状态），关键词 type 可以是 4（事物）或 6（行为）等，但**绝不能是 9**
- 特征 `category_id=1`（人口学特征），关键词 type 可以是 4（事物）或 1（时间）等
```

### 修改 3：`lang/remote/zh-CN/system_prompt.toml` — 添加关键词应原子化的要求

在**关键词提取规则**中，第58-62行之间，新增：

```
**关键词原子化要求**：关键词应当比 feature_text 更细粒度、更原子化。不应直接复制 feature_text 全文作为关键词。
例如：
- feature_text="最近工作压力大" → 关键词应为 ["工作", "压力"]，而非 ["工作压力"]
- feature_text="爱打篮球" → 关键词应为 ["篮球"]，而非直接复制
- feature_text="学过三年中医" → 关键词应为 ["中医"]，而非 ["学过三年中医"]
```

### 修改 4：`lang/remote/zh-CN/system_prompt.toml` — 案例更新

在第94行的示例中，添加一个包含抽象事物的案例：

示例输出中补充：
```json
{
  "category_id": 9,
  "category_name": "近况和状态",
  "feature_text": "最近工作压力大",
  "confidence": 7,
  "keywords": [
    { "type": 1, "word": "最近" },
    { "type": 4, "word": "工作" },
    { "type": 4, "word": "压力" }
  ]
}
```

### 修改 5：`lang/remote/zh-CN/tools/trip_traits.toml` — 更新工具参数描述

**位置**：第13-17行

将 `param_keywords_desc` 和 `param_keyword_type_desc` 中的 "物品" 改为 "事物"：

```
[param_keywords_desc]
other = "与该特征紧密相关的关键词列表。每个关键词包含 type（1-6）和 word（字符串）。type: 1=时间, 2=地点, 3=人, 4=事物, 5=关系, 6=行为。如果无法提取合理关键词可为空数组 []，但不能省略。"
```

```
[param_keyword_type_desc]
other = "关键词类型：1=时间, 2=地点, 3=人, 4=事物, 5=关系, 6=行为"
```

### 修改 6：`lang/remote/en/tools/trip_traits.toml` — 英文同步

对应英文文件中的 type 4 描述也从 "Object" → "Thing/Entity"。

### 修改 7：`internal/remote/agent/toolimp/trip_traits.go` — 更新注释

**位置**：第28行

```go
// TraitKeyword represents a single keyword associated with a trait feature.
// Type values (1-6): 1=时间, 2=地点, 3=人, 4=事物, 5=关系, 6=行为.
type TraitKeyword struct {
    Type int    `json:"type"`
    Word string `json:"word"`
}
```

---

## 不修改的内容

- Go struct 字段名（`Type int` → 保持 `type` 不变，因为这是 JSON 字段名，AI 已习惯输出 `"type"`）
- JSON Schema 中的 `type` 字段（保持 `number` 类型不变）
- 关键词 type 1-3, 5-6 的定义不变
- 特征的 14 类分类体系完全不变

---

## 影响范围

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `lang/remote/zh-CN/system_prompt.toml` | 修改 | type 4 定义扩展 + 添加防混淆警告 + 添加原子化要求 + 添加案例 |
| `lang/remote/zh-CN/tools/trip_traits.toml` | 修改 | "物品"→"事物" 文字更新 |
| `lang/remote/en/tools/trip_traits.toml` | 修改 | "Object"→"Thing/Entity" 文字更新 |
| `internal/remote/agent/toolimp/trip_traits.go` | 修改 | 注释更新 |

**无需修改**：
- Go 业务逻辑（无需改 decode/parse 逻辑）
- JSON schema 定义（type 字段仍是 int，范围由 LLM 自行保证）
- 前端代码
- 数据库 schema

---

## 验证方法

1. 部署后观察 AI 对"工作压力"类特征的提取结果，验证：
   - `keywords[].type` 取值在 1-6 范围内
   - `keywords[].word` 是原子化的（如"工作"、"压力"而非"工作压力"）
   - `keywords[].type` 与 `category_id` 不同（除非恰好巧合）
2. 用测试 prompts 触发特征提取，检查输出 JSON 的合法性
