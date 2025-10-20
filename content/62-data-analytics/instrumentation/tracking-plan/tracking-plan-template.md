---
title: Tracking Plan 模板（事件/属性/身份/合规）
---

用途：规范事件命名、属性字典、身份/会话、版本与合规，作为数据治理与埋点验收的依据。

一、命名规范（示例）
- 事件命名：<动词_名词>，小写蛇形，如 signup_start、purchase_succeeded
- 属性命名：小写蛇形；枚举用 past_tense 或明确枚举值
- 保留字段：user_id、device_id、session_id、event_time、event_id、app_version、os、locale、country、utm_*、source

二、身份与会话
- 身份：优先 user_id，其次 device_id；允许匿名 → 登录后合并（identity merge）
- 会话：默认 30 分钟滚动或 SDK 默认；明确背景活跃/前台切换策略

三、事件规范（YAML 示例）
```yaml
version: 1
owner: growth
namespace: product
rules:
  - event: signup_start
    description: 用户进入注册流程
    trigger: UI 点击 [signup_btn]
    properties:
      - name: plan
        type: string
        desc: 预选套餐 {free|pro}
        required: false
      - name: ab_bucket
        type: string
        desc: A/B 分桶标识
        required: false
    pii: none
    consent: required # 需要同意后采集
  - event: signup_complete
    description: 注册完成
    trigger: 注册成功回调
    properties:
      - name: method
        type: string
        desc: 注册方式 {email|wechat|apple}
        required: true
      - name: referral
        type: string
        desc: 邀请码（脱敏）
        required: false
    pii: low
    consent: required
  - event: purchase_succeeded
    description: 支付成功
    trigger: 支付网关返回成功
    properties:
      - name: order_id
        type: string
        desc: 订单号（哈希处理）
        required: true
      - name: amount
        type: number
        desc: 金额分/分币种另列 currency
        required: true
      - name: currency
        type: string
        desc: ISO 4217
        required: true
      - name: sku
        type: string
        desc: 商品标识
        required: true
    pii: medium
    consent: required
```

四、示例载荷（JSON）
```json
{
  "event": "purchase_succeeded",
  "user_id": "u_123",
  "device_id": "d_abc",
  "event_time": "2025-10-20T12:00:00Z",
  "session_id": "s_xyz",
  "app_version": "1.2.3",
  "os": "ios",
  "locale": "zh-CN",
  "utm_source": "ads",
  "properties": {"order_id": "h_9d3...", "amount": 9900, "currency": "CNY", "sku": "monthly_pro"}
}
```

五、质量与验收清单
- 必填属性全；枚举范围正确；数值单位统一（金额分、时长秒）
- 去重/幂等：event_id 唯一；重试策略不生成重复事件
- 采样策略：高频事件采样率与还原方法记录
- 合规：PII 分类与脱敏；在未同意前不采集；保留期/删除流程
- 回归：埋点随功能变更更新 Tracking Plan；CI 校验 schema
