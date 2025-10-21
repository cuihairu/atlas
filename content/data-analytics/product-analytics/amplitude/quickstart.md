---
title: Amplitude 快速上手（端到端）
---

目标：从埋点到看板的最短路径（Web + 服务端）。

步骤
1) 在 Amplitude 创建 Project，获取 API Key（客户端）与 Secret（服务端）
2) Web SDK（Browser JS）最小埋点
```html
<script type="text/javascript">
!function() {var a=window.amplitude||{_q:[],_iq:{}};function b(a){this._q=[];this._namespace=a}function c(a,b){a[b]=function(){this._q.push([b].concat(Array.prototype.slice.call(arguments,0)));return this}}var d=["add","append","clearAll","prepend","set","setOnce","unset"],e=["setProductId","setQuantity","setPrice","setRevenueType","setEventProperties"],f=["init","logEvent","logRevenueV2","setUserId","setUserProperties","setOptOut","setVersionName","reset"],g=["getDeviceId","getUserId","setDeviceId","setSessionId","resetSessionId"];function h(a){function b(b){a[b]=function(){a._q.push([b].concat(Array.prototype.slice.call(arguments,0)))}}for (var c=0;c<f.length;c++)b(f[c])}h(a);a.getInstance=function(a){a=(a&&0!==a.length?a:"$default_instance").toLowerCase();if(!a||"$default_instance"===a){return a=window.amplitude}return a._iq[a]=a._iq[a]||new b(a)};window.amplitude=a}();
var amp=amplitude.getInstance();
amp.init('YOUR_API_KEY');
amp.setUserId('u_123');
amp.setUserProperties({ plan: 'pro', country: 'CN' });
amp.logEvent('signup_start', { plan: 'pro' });
</script>
```
3) 服务端事件（Node.js 示例，HTTP v2）
```js
import fetch from 'node-fetch';
await fetch('https://api2.amplitude.com/2/httpapi', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
  body: JSON.stringify({ api_key: 'YOUR_API_KEY', events: [
    { user_id: 'u_123', event_type: 'purchase_succeeded', event_properties: { amount: 9900, currency: 'CNY' } }
  ]})
});
```
4) 身份合并与属性
- 登录后 setUserId；匿名阶段使用 device_id；必要时使用 identify API 设置用户属性
5) 验证
- 在 Amplitude → Data → Debugger 查看实时事件；确保属性齐全、枚举正确
6) 看板
- Event Segmentation（活跃/关键行为）、Funnel（注册漏斗）、Retention（D1/D7）
- 保存为 Dashboard；按渠道/国家/版本等维度拆分
7) 治理
- 用 Tracking Plan（本库模板）约束事件；下线废弃事件；变更要有 Owner 与审计

注意
- 合规与同意（CMP）：未同意前禁用采集（setOptOut）
- 幂等与去重：event_id 唯一；重试策略遵循官方建议
