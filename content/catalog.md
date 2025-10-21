---
id: catalog
title: 分类总览（Atlas）
---

本页是知识库的“目录树”。概念与方法只写一处，其他处用链接引用。

- handbook → [[handbook/index|Handbook]] · [[handbook/conventions|Conventions]] · [[handbook/tooling|Tooling]] · [[handbook/reading|Reading]]
- fundamentals → [[fundamentals/algorithms-data-structures|算法/数据结构]] · [[fundamentals/computer-architecture|体系结构]] · [[fundamentals/os-kernel|操作系统]] · [[fundamentals/networking-theory|网络理论]]
- languages → [[languages/c|C]] · [[languages/cpp|C++]] · [[languages/csharp|C#]] · [[languages/java|Java]] · [[languages/go|Go]] · [[languages/lua|Lua]] · [[languages/python|Python]] · [[languages/ts-js|TS/JS]] · [[languages/solidity|Solidity]] · [[languages/swift|Swift]] · [[languages/objective-c|Objective-C]] · [[languages/kotlin|Kotlin]]
- frameworks → web: [[frameworks/ts-js/react|React]] · [[frameworks/ts-js/vue|Vue]] · [[frameworks/ts-js/nextjs|Next.js]] · 小程序: [[frameworks/mini-programs/wechat|微信]] · [[frameworks/mini-programs/alipay|支付宝]] · [[frameworks/mini-programs/bytedance|抖音]] · 跨端: [[frameworks/cross-platform/taro|Taro]] · [[frameworks/cross-platform/uniapp|uni-app]] · [[frameworks/cross-platform/react-native|React Native]] · [[frameworks/cross-platform/flutter|Flutter]] · 移动: [[frameworks/mobile/ios/uikit|UIKit]] · [[frameworks/mobile/ios/swiftui|SwiftUI]] · [[frameworks/mobile/android/jetpack|Jetpack]] · [[frameworks/mobile/android/compose|Compose]] · Go: [[frameworks/go/gin|Gin]] · [[frameworks/go/echo|Echo]] · [[frameworks/go/grpc|gRPC]] · Solidity: [[frameworks/solidity/foundry|Foundry]] · [[frameworks/solidity/hardhat|Hardhat]] · [[frameworks/solidity/openzeppelin|OpenZeppelin]]
- protocols-formats → 传输: [[protocols-formats/network-protocols/transport/tcp|TCP]] · [[udp]] · [[quic]] · 安全: [[protocols-formats/network-protocols/secure/tls|TLS]] · HTTP: [[http1]] · [[http2]] · [[http3]] · [[https]] · 双向: [[websocket]] · [[rsocket]] · RPC: [[grpc]] · [[http-rpc]] · [[thrift]] · 序列化: [[protobuf]] · [[flatbuffers]] · [[capnproto]] · [[msgpack]] · [[json]] · 可执行: [[pe]] · [[elf]] · [[macho]] · 设备总线: [[i2c]] · [[spi]] · [[uart]] · [[can]] · 金融协议: [[fix]] · [[sbe]] · [[itch]] · [[ouch]] · [[fast]] · 工业协议: [[modbus]] · [[opc-ua]] · [[profinet]] · [[ethercat]] · [[ethernet-ip]] · [[canopen]] · EVM: [[abi]] · [[bytecode]] · [[opcodes]] · [[json-rpc]]
- systems → [[systems/linux|Linux]] · [[systems/windows|Windows]] · [[systems/networking|Networking]] · [[systems/storage|Storage]] · 虚拟化: [[systems/virtualization/kvm|KVM]] · [[systems/virtualization/qemu|QEMU]]
- embedded-realtime → [[mcus]] · [[rtos]] · [[drivers]] · [[comm-stacks]] · [[bootloader-ota]] · [[safety]]
- infrastructure → 容器: [[containers/docker|Docker]] · 编排: [[orchestration/k8s|K8s]] · 代理/LB: [[nginx]] · [[lvs]] · [[haproxy]] · [[envoy]] · 发现: [[nacos]] · [[etcd]] · [[consul]] · 消息: [[kafka]] · [[rabbitmq]] · [[nats]] · [[pulsar]] · IaC: [[terraform]] · [[ansible]] · [[packer]] · 工业网关: [[ot-gateways]]
- data-systems → 关系: [[mysql]] · [[postgresql]] · NoSQL: [[mongodb]] · 缓存: [[redis]] · 分析: [[duckdb]] · [[clickhouse]] · 管道: [[pipelines]] · 搜索: [[search]] · 时序: [[timescaledb]] · [[influxdb]] · [[questdb]]
- data-analytics → [[data-analytics|数据分析]] · 概念: [[data-analytics/concepts/metrics-taxonomy|指标命名]] · [[data-analytics/concepts/funnels-cohorts-retention|漏斗/留存]] · [[data-analytics/concepts/ltv-churn|LTV/流失]] · 埋点: [[data-analytics/instrumentation/tracking-plan|Tracking Plan]] · 平台: [[data-analytics/product-analytics/amplitude|Amplitude]] · [[data-analytics/product-analytics/mixpanel|Mixpanel]] · 网站: [[data-analytics/web-analytics/ga4|GA4]] · [[data-analytics/web-analytics/plausible|Plausible]] · 游戏: [[data-analytics/game-analytics/metrics|游戏指标]] · 实验: [[data-analytics/experimentation/ab-testing|A/B]]
- reverse-engineering → [[binary-formats (see 30/file-formats/executable)]] · 工具: [[ghidra]]/[[ida]]/[[radare2]]（未来补充） · 动态: [[frida]]/[[objection]]（未来补充）
- distributed-systems → [[theory]] · [[consensus]] · [[data]] · [[transactions]] · [[time-clocks]] · [[ids]]
- concurrency-parallelism → [[models]] · [[sync-primitives]] · [[memory-models]] · [[scheduling]] · [[patterns]]
- architecture-design → [[system-design]] · [[microservices]] · [[domain-driven-design]] · [[api-design]] · [[reliability]] · [[security]] · [[patterns]]
- observability → [[metrics-logs-tracing]] · [[monitoring-alerting]] · 栈: [[prometheus]] · [[grafana]] · [[loki]] · [[tempo]] · [[otel]] · [[chaos]] · [[slis-slos-error-budgets]]
- performance-capacity → [[benchmarking]] · [[load-testing]] · [[tuning]] · [[latency-budgets]] · [[capacity-planning]]
- engineering-practices → [[testing]] · 调试: [[gdb]] · [[lldb]] · [[windbg]] · [[perf]] · [[ebpf]] · 网络分析: [[wireshark]] · [[tcpdump]] · [[tshark]] · [[mitmproxy]] · [[burp]] · 版本管理: [[git]] · [[svn]] · 构建: [[cmake]] · [[maven]] · [[gradle]] · [[npm-pnpm]] · CI/CD: [[github-actions]] · [[gitlab-ci]] · [[argo]] · [[tekton]] · 项目管理: [[scrum]] · [[kanban]] · 质量: [[lint]] · [[format]] · [[static-analysis]] · [[security-scans]]
- domains → [[game-server]] · [[blockchain]] · [[mobile-apps]] · [[web-crawling]] · [[industrial-automation]] · [[quantitative-finance]]
- templates-snippets → 模板与脚手架（见下方模板）
- scratch → 草稿临时区

标签建议：#cap/concurrency #cap/distributed #cap/observability #cap/performance #cap/security #proto/http2 #ser/protobuf #db/mysql #infra/k8s #domain/game #domain/blockchain #domain/ics #domain/quant。
- data-analytics 模板/示例：[[data-analytics/instrumentation/tracking-plan/tracking-plan-template|Tracking Plan 模板]] · [[data-analytics/game-analytics/metrics/dashboard|游戏指标仪表盘]] · [[data-analytics/product-analytics/amplitude/quickstart|Amplitude 快速上手]]