# 多角度图片逐张回传设计

## 背景

启用“多角度分别生成”后，GPT 图片模型和 Gemini 图片模型都会把多个角度放入一个插件 job。GPT 在该 job 内顺序调用上游接口，直到全部角度结束才写回历史结果；Gemini 将多个角度作为一个 batch 的多个 item，但只在 batch 总体完成后读取 item。两条路径都会使已完成图片被延迟展示。

## 目标

- GPT 多角度分别生成时，每个角度独立提交并在完成时立即显示。
- Gemini 多角度分别生成时，batch 未完成但已有 item 完成时立即显示对应图片。
- 多个角度仍在一条用户消息和一条助手消息中展示。
- 取消、失败、重试和历史恢复保持与现有分组任务语义一致。
- 不修改主站服务、数据库结构或对外 API 路径。

## 设计

### GPT

前端将“多角度分别生成”的 GPT 请求纳入现有独立 GPT 任务分支。每个角度发起一个 `/generate` 请求，携带单一角度 prompt、`output_count: 1` 和共享的 `generation_group_id`。每个请求获得独立 history job；现有槽位更新、取消、重试和历史分组逻辑按 job 逐个工作。

Gemini 的多角度仍发送一个带 `variants` 的请求，不改变 batch 提交形态。

### Gemini

后端在每次 batch 状态查询时，即使 batch 仍在 `pending`，也读取 batch items。对于当前 history record 的已完成 item：读取图片内容、归档后写入 `record.Result.images`，并按 item custom ID 记录已归档项，防止后续轮询重复下载或重复写图。

batch 仍未完成时，history record 保持 `pending`，但 status 响应携带已有的 `result.images`。全部 batch 完成时沿用现有终态收尾逻辑，并保留已归档图片；失败 item 在 batch 终态时写入 `failed_variants`。

### 前端轮询

收到 `pending` 状态时，前端先将响应中的图片按角度标签或槽位位置写入空槽位，再继续轮询。已填充槽位不被后续 pending 响应覆盖。

## 错误与边界

- GPT 单个角度失败不阻塞其他角度展示。
- Gemini item 查询不返回已完成图片时，维持现有等待行为。
- Gemini 增量归档失败时，status 请求返回错误并由前端按现有重试轮询处理；不将整个 job 误标为成功。
- 已归档 Gemini item 在后续轮询和最终完成时不会重复下载或产生重复图片。

## 测试

- 前端：GPT 两个独立角度发出两次请求、共享分组 ID，先完成的一张立即进入对应槽位。
- 前端：pending 状态响应携带一张图片时，槽位立即显示并继续轮询。
- 后端：pending Gemini batch 的 items 包含一个完成 item 时，status 保持 pending 但结果含该图片；下一次相同 items 不重复读取内容；batch 完成时保留该图片并补齐其余图片或失败标签。
- 运行图片插件前端全量测试、Go 图片插件测试、前端构建和生成资产一致性校验。
