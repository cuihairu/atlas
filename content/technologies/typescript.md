---
id: 20241020-tech-001
title: TypeScript 速记
tags: [typescript, language]
created: 2024-10-20
updated: 2024-10-20
---

常用片段：

```ts
// Narrowing + 类型守卫
type Input = string | number
function toNumber(x: Input): number { return typeof x === 'number' ? x : Number(x) }
```

参见 [[projects/quartz-setup]]。
