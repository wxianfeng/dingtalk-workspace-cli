---
category: Added
---

- **Interactive card callback events** ([Aone 86136546](https://project.aone.alibaba-inc.com/v2/project/2125919/req/86136546)) — `dws event consume user_card_action_triggered` reuses the personal-event subscription lifecycle with the same empty-object filter rule as IM/OA. Its flattened schema now describes the reviewed callback fields under `payload.body.actionData.context`, including typed dynamic answers, questions, business/conversation context, operator identity, and millisecond timestamps, while preserving unknown fields for forward compatibility.
