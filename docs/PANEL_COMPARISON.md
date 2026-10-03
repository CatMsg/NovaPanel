# NovaPanel、s-ui、3x-ui：源码对比与取舍

## 比较范围

这份比较针对相同的日常任务：配置代理、维护用户、分享订阅、调整分流、保存配置和排查故障，不以协议数量或截图美观决定优劣。没有在生产 VPS 上替换核心、迁移账号或测量上游性能。

2026-10-02 检查的源码快照：

| 项目 | 源码版本 | 当时最新稳定 Release |
| --- | --- | --- |
| NovaPanel | `43b17a9` / v1.6.126，加本次工作区改动 | v1.6.126 |
| s-ui | [`84854f8`](https://github.com/alireza0/s-ui/tree/84854f87b21a7e1b494bc26d00643b782c3f70e2)，前端子模块 [`f859e16`](https://github.com/alireza0/s-ui-frontend/tree/f859e16953cd733293618f626cc19b8466e00fd3) | [v1.6.3](https://github.com/alireza0/s-ui/releases/tag/v1.6.3) |
| 3x-ui | [`a716122`](https://github.com/MHSanaei/3x-ui/tree/a716122ef2e6e63c083ef92142ffe9806a430fe7) | [v3.8.5](https://github.com/MHSanaei/3x-ui/releases/tag/v3.8.5) |

上游代码采用 main 快照；不表示这些 main 功能全部已进入上述稳定 Release。版本、功能和界面会变化，后续参考时应重新核对。

## 结论与取舍

| 领域 | 比较依据 | NovaPanel 的决定 |
| --- | --- | --- |
| 核心与协议 | s-ui 同属 sing-box；3x-ui 的大量配置围绕 Xray 和协议专属辅助服务 | 保留 sing-box 架构，不照搬 Xray 字段、旁路核心或更换前端框架。协议更多不等于当前链路更稳定 |
| 统一用户 | NovaPanel 已按用户统一管理多入站、限速、配额、历史和外部协议 | 保留统一身份，不为借鉴上游而重复建立按入站拆分的账户模型 |
| 分享流程 | [s-ui QrCode.vue](https://github.com/alireza0/s-ui-frontend/blob/f859e16953cd733293618f626cc19b8466e00fd3/src/layouts/modals/QrCode.vue) 将多个大二维码堆叠；[3x-ui ClientQrModal.tsx](https://github.com/MHSanaei/3x-ui/blob/a716122ef2e6e63c083ef92142ffe9806a430fe7/frontend/src/pages/clients/ClientQrModal.tsx) 按选择展示二维码，处理加载和过长内容 | 采用单二维码、格式切换、明确复制入口和错误/空状态；保留 NovaPanel 自己的订阅格式及暂停访问、轮换凭据 |
| 请求生命周期 | [3x-ui queryClient.ts](https://github.com/MHSanaei/3x-ui/blob/a716122ef2e6e63c083ef92142ffe9806a430fe7/frontend/src/queryClient.ts) 将查询与写操作区分，mutation 不自动重试；[s-ui api.ts](https://github.com/alireza0/s-ui-frontend/blob/f859e16953cd733293618f626cc19b8466e00fd3/src/plugins/api.ts) 取消重复请求的做法仍涉及写操作 | 借鉴读写分离，不引入 React/TanStack。普通数据 GET 只共享进行中的响应；POST 不自动取消、合并或重试，写操作使旧读取不再可共享 |
| DNS 与流量规则 | NovaPanel 已有规则目录、DNS 分流、DNS 命中诊断和系统流量规则 | 保留 DNS 解析与流量出口的区别，不把两类规则合成一个含糊的“代理规则”。分流流程补实际操作说明 |
| 保存与故障恢复 | 本地 `service/preflight.go`、`service/config_snapshot.go` 已实现预检和失败恢复；前端保存先预检再提交 | 保留服务器端校验和回滚，不用浏览器成功提示替代核心启动验证，不盲目自动重试写请求 |
| 多服务器运维 | 本地已有服务器状态、周期流量、模板差异预览和灰度下发 | 保留这些运维能力；不引入第二套远端用户或节点聚合模型，不重新启用已移除的坏功能 |
| 文档组织 | [s-ui 官方 Wiki](https://github.com/alireza0/s-ui/wiki) 将 API、对象配置及订阅模板分开说明 | 采用任务导向的本地操作文档，先覆盖分享、DNS 分流、保存验证和批量下发，避免只罗列功能名 |
| 独立订阅标识 | [3x-ui SubLinksModal.tsx](https://github.com/MHSanaei/3x-ui/blob/a716122ef2e6e63c083ef92142ffe9806a430fe7/frontend/src/pages/clients/SubLinksModal.tsx) 使用 `subId` 而非显示名称 | 有价值，但涉及已有订阅兼容与迁移，本次不悄悄改变订阅标识或让旧链接失效 |
| 安全模块与数据库并发 | s-ui 有密码哈希和不同 SQLite 连接设置；3x-ui 有更多认证集成 | 按用户要求不新增安全能力模块。不照搬 SQLite 连接数，不在无测量和迁移方案时宣称性能提升 |

## 本次 UI 改进

| Before | After | Why |
| --- | --- | --- |
| 自动、JSON、Clash、sing-box 四个二维码纵向堆叠 | 默认 Clash/Mihomo，选择一个格式只显示一个二维码 | 减少误扫和手机长滚动，当前格式清晰可见 |
| 每个节点都显示二维码 | 节点选择框配一个二维码 | 多节点用户也能快速定位要分享的节点 |
| 首次异步加载弹窗可能没有触发用户读取 | 首次打开立即加载，换用户/关闭时使旧响应失效 | 防止空数据和误分享前一个用户 |
| 缺订阅地址或读取失败仍尝试生成二维码 | 明确空状态、失败重试，禁用空链接复制 | 不给用户一个无效二维码 |
| 长链接可能超过二维码容量 | 提示改用复制，仍保留完整链接 | 错误状态可恢复，不让整个弹窗崩溃 |
| 分享界面的旧凭据轮换将原地修改函数的空返回值赋给配置 | 克隆配置、原地轮换、保存完整配置 | 避免遗漏配置，同时保留用户名和其他参数 |

本次只借鉴交互与工程原则，未复制上游组件实现或引入上游依赖。没有进行线上发布或服务器配置修改。

## 验证入口

- 请求并发、失败重试及写后读取隔离：`frontend/src/plugins/api.test.ts`。
- 分享首次打开、格式和节点切换、复制、桌面/手机布局、关闭后迟到响应、失败/空状态、长链接和凭据保留：`frontend/tests/e2e/clients.spec.ts`。
- 实际流程：[操作指南](WORKFLOWS.md)。

```sh
cd frontend
npm run test:unit
npm run typecheck
npm run lint:check
npm run test:e2e
npm run build
```

2026-10-02 本地结果：11 项单元测试、18 项 mock API 端到端测试、类型检查、Lint（150 个图标检查）、前端构建和 Go 全套测试均通过；开发脚本完成原生后端构建，2095 登录页返回 HTTP 200。Lint 首次与端到端测试并行时遇到生成目录被清理的文件扫描错误，测试完成后重跑已通过。

内置浏览器在本次会话返回 `Browser is not available: iab`，因此未完成真实本地页面的手工视觉验收；未改用外部浏览器。生产 VPS、客户端握手和服务商解锁情况不在本次验证范围内。
