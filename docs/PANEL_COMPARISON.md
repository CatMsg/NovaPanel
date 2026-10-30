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
| 多服务器运维 | 本地已有服务器状态、周期流量和远程新建入站 | 从空白协议草稿在目标机独立创建入站，使用目标机证书和本地用户；配置模板与灰度下发已移除 |
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

## 2026-10-11 补查

以下是新一轮源码检查，不替换上面的历史结果。比较的是固定 main 快照，不把 main 等同于稳定版，也没有在 VPS 上安装另外两个面板。

| 项目 | 本轮源码快照 | 查询时的稳定 Release |
| --- | --- | --- |
| NovaPanel | `b740c2e` / v1.6.128，加本轮未提交补丁 | 本轮未发布 |
| s-ui | [`8586674`](https://github.com/alireza0/s-ui/tree/85866744774f9c1640f1292ac30baea2f72b438e) | [v1.6.4](https://github.com/alireza0/s-ui/releases/tag/v1.6.4) |
| 3x-ui | [`442b7fb`](https://github.com/MHSanaei/3x-ui/tree/442b7fb1637d6ecf4f2c28f76092228856e766c9) | [v3.9.0](https://github.com/MHSanaei/3x-ui/releases/tag/v3.9.0) |

### 日常任务对比

| 日常任务 | 上游可借鉴的依据 | NovaPanel 现状与本轮决定 |
| --- | --- | --- |
| 删除配置对象前了解影响 | 3x-ui 的 [reference-cleanup.ts](https://github.com/MHSanaei/3x-ui/blob/442b7fb1637d6ecf4f2c28f76092228856e766c9/frontend/src/pages/xray/reference-cleanup.ts) 先规划出站、均衡器删除影响，再处理引用 | 出站、规则集和 DNS 均有引用校验；DNS 删除/改名提供依赖提示并拒绝歧义或仍被引用的变更，不自动级联删除用户规则 |
| DNS 与流量出口分流 | s-ui 与我们同属 sing-box 配置模型；3x-ui 则是 Xray DNS/routing | 保留现有 DNS 分流、规则目录和 DNS 命中诊断。只修 DNS 标签引用，不改变 UDP/TCP 选择，不把解析器当作代理出口 |
| 多协议用户、流量配额、重置和有效期 | 上游都有配额/有效期管理，但其账户和统计模型不同 | 本地 `service/client.go` 已统一多入站身份、月初重置、首次使用有效期；`service/mieru_clients.go`、`service/masque_clients.go` 接入外部协议。订阅信息页 `sub/user_center.go` 已显示每月 1 日重置和面板时区；首次使用后的有效期仍按天计算 |
| 分享订阅及控制展示内容 | 3x-ui [model.go](https://github.com/MHSanaei/3x-ui/blob/442b7fb1637d6ecf4f2c28f76092228856e766c9/internal/database/model/model.go) 的 `Inbound.ExcludeFromSub` 可隐藏订阅内入站但不停止服务 | NovaPanel 已有多格式订阅、单二维码切换、统一用户和集合订阅；入站可单独从订阅中隐藏，服务仍保持运行 |
| 备份与恢复 | 3x-ui [备份文档](https://github.com/MHSanaei/3x-ui/blob/442b7fb1637d6ecf4f2c28f76092228856e766c9/docs/content/docs/en/operations/backup-restore.mdx) 支持定时 Telegram/Discord 异机备份 | NovaPanel 已有默认关闭的 WebDAV 自动异地备份、保留策略、完整性清单和只读恢复演练；真实 WebDAV 服务商恢复仍需外部验收 |
| 临时停核心但保留面板维护 | s-ui [service/config.go](https://github.com/alireza0/s-ui/blob/85866744774f9c1640f1292ac30baea2f72b438e/service/config.go) 的 `SetMaintenance` 和 `lifecycleMu` 使维护状态持久化，阻止 watchdog 自动重启 | NovaPanel 已有持久化维护模式，停用代理数据面同时保留面板与订阅服务，并阻止 watchdog 或配置保存意外重启核心；生产 VPS 操作仍需单独验收 |
| 实时排障、历史和故障切换 | 不同核心具有不同连接统计与路由接口，不能简单复制字段 | 已有 `service/sessions.go`、用户历史、流量图和 `service/failover.go`；本轮未发现需要为了功能数量引入第二套统计/核心的理由。单元测试不等于 VPS 真实握手或 DNS 服务商验收 |
| 多 VPS 管理与远程创建 | 单机面板功能不能替代集合运维 | `service/fleet.go`、`service/fleet_inbounds.go` 提供集合状态、配置差异及单台/批量远程新建入站。全部目标先预检，再逐台创建；配置模板与灰度下发已移除 |
| 前端请求与编辑一致性 | 参考上轮的读写生命周期分析 | `frontend/src/plugins/api.ts` 已处理读写隔离。本轮改名基于完整配置草稿，外部独立保存资源引用旧标签时拒绝改名，避免界面改了但数据库没改 |

### 本轮已补的缺口

1. **DNS 删除保护**：删除确认列出默认 DNS、DNS 分流、路由解析动作和命名解析器引用；被引用时禁止删除，执行前再次检查。提示移到可滚动的独立对话框，避免挤在小卡片中。
2. **DNS 改名一致性**：在同一配置内迁移引用并保留解析策略等参数；独立保存的入站、出站、节点或服务仍有引用时给出具体位置并拒绝改名。空标签、重复标签及过期编辑也拒绝保存。
3. **后端兜底**：保存/预检校验 DNS 标签、引用和显式解析器依赖环，错误带字段路径；兼容核心的匿名索引标签及默认 local 解析器生命周期。仅遍历已知配置容器，不修改请求头或 hosts 自定义映射。
4. **订阅信息页**：自动重置统一显示“每月 1 日”，使用面板时区显示下次重置；未启用自动重置时，首次使用后的有效天数保持原行为。

### 2026-10-11 后续可选项（历史计划）

当时的优先级建议是自动异机备份与恢复演练、明确的维护模式、订阅隐藏开关；这些功能已在 2026-10-13 补充中记录为实现。独立随机订阅标识没有采用，且用户已确认继续使用用户名订阅 URL，不属于待实现项。没有新加安全能力模块、Xray 专属字段或旁路核心，也没有做无依据的性能提升承诺。

### 本轮验证

本轮测试和本地运行结果以项目根目录的 `log.md` 最新条目为准；源码比较不证明上游页面已实测，也不证明生产 VPS 行为。所有改动保持未提交、未发布、未部署。

## 2026-10-13 补充：四项本地实现与验收

本节只更新本轮工作区状态，不改写 2026-10-02 和 2026-10-11 的历史比较或验证，也不表示已发布或部署。

| 领域 | 当前实现与验证边界 |
| --- | --- |
| MASQUE | 基于固定官方 sing-box `v1.15.0-alpha.12`，三处审核文件及补丁均固定哈希与精确重放；共用原生数据面，不恢复自研 TUN/NAT。sing-box 使用 H3/TLS/账号密码，JSON 输出 `endpoints`；可选 Mihomo ECDSA 密钥认证，Clash 输出原生路径/独立用户密钥/固定地址。同一密钥仅一个同时在线隧道。禁止 peer route/forward/ICMP，Mihomo 精确默认通告仅丢弃。真实 Mihomo 1.19.31 的本地双客户端 TCP/UDP、统计、撤销和配置解析有验收门禁；未验证公网互通或性能。不提供虚构通用 URI。 |
| 异地备份 | 默认关闭，严格校验 HTTPS WebDAV 证书；本地 IAB 确认默认关闭且无目标时操作禁用。演练使用独立快照，执行完整 SQLite `integrity_check` 和与正式恢复共享的 fleet JSON 接受校验，不写 live DB、不启动快照服务。没有真实 WebDAV 目的地或外部凭据恢复验证。 |
| 维护模式 | 持久维护状态和流量暂停路径已完成本地 UI、单测与 race 回归；管理面板和订阅服务保持可用。无 VPS 部署验证。 |
| 订阅隐藏 | `exclude_from_sub` 只影响后续订阅过滤，入站继续运行，不轮换凭据或撤销既有客户端配置；本地 UI、单测与 race 回归已验证。 |

本地验证：前端全套 E2E 36 项、单测 46 项、typecheck、lint 与生产构建通过。默认 Go 全包测试、核心与业务层 race、vet 和发布所用 staticcheck 通过；QUIC 测试强制执行，不可用时仅跳过对应运行态子测试。聚合原生配置启动的并发修复连续 20 次回归通过。本地 IAB 验证维护状态跨进程保留及恢复，原数据库数据保留。未发布、未部署，未验证真实 WebDAV 目的地、外部凭据恢复或公网客户端互通；没有性能提升测量。

## 2026-10-30 状态复核

上面的 2026-10-02、10-11 和 10-13 比较快照保留当时的上游源码版本与测试结果，不代表本日重新比较了 s-ui/3x-ui 的完整源码。下表核对 NovaPanel 当前工作树状态；本日上游 Release 说明另见后文。

| 领域 | 当前证据 | 仍未验证或处理 |
| --- | --- | --- |
| MASQUE | sing-box 已固定为官方 `v1.15.0-alpha.13`，来源、补丁、回归文件和 Cronet pin 有完整哈希校验；本地 sing-box race 和 Mihomo 1.19.31 TCP/UDP 互通门禁通过。 | 未做公网客户端互通、性能测量或生产 VPS canary；1.16 正式版尚未进入升级验收。 |
| 异地备份 | WebDAV 自动备份默认关闭；配置、状态、密钥写入保护、快照完整性与隔离恢复演练均有实现和 UI/E2E 覆盖。 | 未接入真实 WebDAV 服务商，未验证真实凭据、远端恢复与服务商行为。 |
| 维护模式 | 持久状态、暂停代理流量、阻止 watchdog 意外重启的路径已有实现及本地回归。 | 未在生产服务器验证维护/恢复操作。 |
| 订阅隐藏 | 入站 `exclude_from_sub` 只过滤后续订阅内容，不停止入站、不轮换凭据；已有前后端测试。 | 不撤销客户端已导入的旧配置，这是预期行为。 |
| 发布门禁 | v1.6.149 已发布；GitHub Actions `40975087801` 全部通过，包含 Windows 安装器检查及 Linux 原生安装、UFW/ACME 验收。Linux 发布仅支持 amd64。 | 真实 WebDAV 服务商恢复、公网 MASQUE 互通、生产维护模式操作和 ACME 实际签发/续期仍需外部验收。 |
| 订阅标识 | 继续采用用户名订阅 URL，按用户确认未改成随机标识。 | 无迁移或旧链接过期工作。 |
| DNS 查询匹配 | 当前 DNS 规则编辑器支持 Sing-Box `query_type`、`query_client_subnet`、`query_dnssec`，并保留旧标量/列表写法。 | 客户端是否实际发送 EDNS Client Subnet 或 DNSSEC DO 位取决于查询端，UI 条件不保证外部查询会命中。 |
| Linux 架构 | 用户确认服务端发行仅支持 amd64；安装器检测、Release 构建矩阵和文档现统一限制为 amd64；v1.6.149 hosted Linux 验收已通过。 | 无其他 Linux 架构承诺；非 amd64 主机不在支持范围内。 |
| Windows 平台范围 | 用户于 2026-10-30 澄清目前没有 Windows 平台计划；本轮目标按 Linux amd64 服务端处理。 | 仓库内既有 Windows 源码/CI 文件保留但不扩展；历史 Windows CI 结果不代表当前产品适配承诺，也不纳入本轮完成条件。 |

当前优先补齐项不是再增加面板功能，而是安排真实 WebDAV 恢复验收、受控公网 MASQUE/canary，以及生产维护模式和 ACME 签发/续期演练。v1.6.149 曾通过 Windows/Linux hosted gates；这是历史验证，不扩大当前 Linux amd64 目标。所有线上验收均需单独授权，本地或 CI 检查不等同于生产部署。

### 2026-10-30 上游 Release 复查

截至本日读取官方 GitHub Release 页面：[s-ui v1.6.6](https://github.com/alireza0/s-ui/releases/tag/v1.6.6) 于 10 月 24 日发布；[3x-ui v3.11.0](https://github.com/MHSanaei/3x-ui/releases/tag/v3.11.0) 于 10 月 25 日发布。这里复核的是 Release 说明和与当前代码直接相关的功能点，不是对两套上游源码的完整逐文件比较。

| 上游变化 | 与 NovaPanel 的关系 | 结论 |
| --- | --- | --- |
| s-ui 修复 QUIC 用户会话来源地址在 NAT 重绑定后长期留存，并补充外部 Clash 订阅的 XHTTP 参数保留 | s-ui 改的是独立用户会话来源注册表；NovaPanel 的 `ConnTracker` 跟踪 sing-box 路由后的 TCP/UDP 连接，并在包装连接关闭或读写终止时移除记录，生命周期模型不同 | 暂无证据表明是同一类缺陷，不移植清理策略；需要时应针对 NovaPanel 的 QUIC 在线用户/历史另设回归，而不能把实时连接表当作用户会话注册表 |
| 两家上游都改善 XHTTP 到 Clash/Mihomo 订阅的参数传递；3x-ui 还明确提到传递 XMUX 设置 | NovaPanel 的聚合器只对原始 Clash 节点做格式过滤和名称合并，不应丢弃未知的嵌套传输参数 | 新增 `TestConvertRawClashProxiesPreservesXHTTPOptions`，覆盖 XHTTP、TLS/ALPN、headers、padding、reuse-settings 和 download-settings；CI/发布脚本将真实生成配置交给固定摘要的 Mihomo 1.19.31 `-t` 解析，且全套 MASQUE TCP/UDP 门禁通过 |
| 3x-ui v3.11.0 的缓存、索引、gRPC 连接池及 AmneziaWG 优化 | 主要依赖 Xray、特定内核数据面或大规模部署负载 | 不按 Release 文案直接移植；先有 NovaPanel 自身可复现的性能剖析和基准数据再立项。其安全改动不在本次范围内 |

**支持边界：**NovaPanel 当前验证的是把外部 Mihomo XHTTP 节点配置原样聚合，并由 Mihomo 解析通过；没有验证与真实 XHTTP 服务端连通，也不代表面板可原生创建 XHTTP 入站。当前官方 sing-box [V2Ray transport 配置](https://sing-box.sagernet.org/configuration/shared/v2ray-transport/)列出 HTTP、WebSocket、QUIC、gRPC 和 HTTPUpgrade，没有 XHTTP。原生入站/订阅生成仍是未支持项，不应在没有核心或数据面支持时伪装成已完成。Mihomo 的 XHTTP 和 XMUX 字段以其[官方传输配置](https://wiki.metacubex.one/en/config/proxies/transport/)为准。
