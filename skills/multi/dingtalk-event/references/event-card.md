# 互动卡片回调事件

先读事件产品入口 [SKILL.md](../SKILL.md) 的命令规则、生命周期和失败处理。本参考覆盖公开个人事件 `user_card_action_triggered`。

<!-- dws-intent: event.listen.card -->互动卡片业务发生回调时，使用 `dws event consume` 长连接实时监听；不要轮询卡片状态模拟事件。

## 订阅规则

- 使用当前用户 OAuth 身份，非默认组织加全局 `--profile <corpId 或 profile 名>`。
- 注册复用 IM 的 `POST /dws/subscription/user`，取消复用 `POST /dws/subscription/cancel`。
- 事件使用 `ruleType=all`，并与 IM/OA 全量事件一致发送 `filterRule={}`。
- 不接受 `--user`、`--open-dingtalk-id`、`--group`、`--query`、`--filter-json` 或 `--role-types`。

## Golden Route

```bash
dws event list --category card
dws event schema user_card_action_triggered --flatten
dws event consume user_card_action_triggered --flatten -f ndjson
```

临时验证可加 `--max-events 1` 或 `--duration 10m`。等待 `[event] ready event_key=user_card_action_triggered ...` 后再触发卡片交互；干净退出会自动取消本次新建的订阅。复用既有订阅时使用 `--subscribe-id`，外部停止先运行 `dws event stop <subscribe_id> --dry-run`，确认后再加 `--yes`。

## 扁平输出

`--flatten` 只承诺以下结构：

```json
{
  "type": "user_card_action_triggered",
  "event_id": "...",
  "timestamp": 0,
  "subscribe_id": "...",
  "payload": {}
}
```

`payload` 是开放对象，保留服务端推送的未知业务字段。不要臆造卡片实例、操作者、动作或表单字段；需要某个业务字段时以实时 payload 和 `dws event schema user_card_action_triggered --flatten` 为准。不传 `--flatten` 时保持兼容 transport envelope，业务数据仍在字符串 `.data` 内。

该事件与其它无目标个人事件可放入同一个 consume，共享 personal bus；每个 EventKey 仍创建独立订阅。启动中任一订阅失败时，现有多事件生命周期会回滚本次已创建项，并沿用同一重试保护。
