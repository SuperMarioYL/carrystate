<div align="right"><sub><a href="./README.en.md">English</a>&nbsp;&nbsp;⇄&nbsp;&nbsp;<b>简体中文</b></sub></div>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/hero-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/hero-light.svg">
  <img src="./assets/hero-light.svg" width="880" alt="CarryState — pack agent state across harnesses, airgapped and verifiable">
</picture>

<p align="center"><sub>面向信创 / 数据不出境企业的本地 Agent harness 状态迁移工具——把 Claude Code 等 harness 的 sessions、memories、MCP config 打包成可校验的迁移 bundle，导入新 harness 全程不出内网。</sub></p>

<p align="center">
  <a href="./LICENSE"><img alt="license" src="https://img.shields.io/github/license/SuperMarioYL/carrystate?color=blue"></a>
  <img alt="release" src="https://img.shields.io/github/v/release/SuperMarioYL/carrystate">
  <img alt="ci" src="https://img.shields.io/github/actions/workflow/status/SuperMarioYL/carrystate/ci.yml?branch=main&label=ci">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white">
  <img alt="Agent-ready" src="https://img.shields.io/badge/Agent-ready-5E5CE6">
  <img alt="信创 airgap" src="https://img.shields.io/badge/%E4%BF%A1%E5%88%9B-airgap-10A37F">
</p>

---

**切 harness = 冷启动，丢学到的 context；CarryState 把 agent state 打包带走，全程不出内网。**

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/atlas-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/atlas-light.svg">
  <img src="./assets/atlas-light.svg" width="880" alt="架构：Claude Code pack → .csb Bundle → Codex unpack，merkle 校验">
</picture>

## 目录

- [为什么需要](#为什么需要)
- [架构](#架构)
- [安装](#安装)
- [快速开始](#快速开始)
- [用法](#用法)
- [Demo](#demo)
- [对比 cc-switch](#对比-cc-switch)
- [付费 / 企业版](#付费--企业版)
- [路线图](#路线图)
- [贡献](#贡献)

## 为什么需要

信创 / airgapped 企业 ops 团队在内网-only 机器上、在「数据不出境」合规要求下切换或升级本地 coding-agent harness（Claude Code → Codex → OpenCode）时，面临冷启动：新 harness 不拥有前一个 harness 的 sessions、学到的 memories、MCP server config，每次切换都丢掉积累的 context。[Hoplite (YC S26)](https://hoplite.sh) 明确把 "we port over your local setup — sessions, memories, MCP servers" 做成了付费产品，但它的目的地是自己的云——信创内网企业结构性地用不了它。[farion1231/cc-switch](https://github.com/farion1231/cc-switch)（124k 星）切换 launcher 但**不迁任何 state**，每次切换都是冷启动。

CarryState 补的就是这个缺口：一个本地、harness-agnostic 的 state 互换 bundle——sessions + memories + MCP config 打包、schema 翻译、导入新 harness，**全程不出内网**。它对标 Hoplite（Yc S26）的云端 state-porting，把方向留在本地；与 cc-switch 互补——cc-switch 切 harness，CarryState 迁它留下的 state。Agent state 在 harness 间可迁移这一需求已被 [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) 这类被 cc-switch 列为切换目标的多 harness 生态验证。

## 架构

一个静态 Go 二进制 `carrystate`，三个子命令，四个内部包——没有 daemon、没有网络、没有微服务、没有 K8s。这是 Airgap 的**结构保证**，不是配置选项。

- `cmd/carrystate`（cobra: pack / unpack / verify）
- `internal/bundle`——`.csb` 读写 + merkle root
- `internal/harness`——adapter 注册表 + claudecode / codex
- `internal/checksum`——每 entry sha256
- `internal/translate`——native ↔ bundle schema 翻译

唯一 I/O 是读源 harness 的 state dir、写目标 harness 的 state dir。`verify` 重跑 translator 的确定性子集并比对 checksum——v0.1 不上 LLM。

## 安装

```bash
go install github.com/SuperMarioYL/carrystate@latest
```

或者从 [Releases](https://github.com/SuperMarioYL/carrystate/releases/latest) 拿一个预编译二进制（linux/darwin/windows × amd64/arm64），USB 拷进内网即可——零依赖、零联网。

## 快速开始

```bash
# 1. pack：~/.claude → project.csb（sessions + memories + MCP，merkle 校验）
carrystate pack --harness claudecode --state-dir ~/.claude --out project.csb

# 2. verify：重算每个 sha256 + merkle root（绿勾 = 可发运）
carrystate verify --bundle project.csb

# 3. unpack：project.csb → ~/.codex（Codex 原生布局，无损）
carrystate unpack --harness codex --in project.csb --state-dir ~/.codex
```

<details><summary>样例输出</summary>

```
$ carrystate pack --harness claudecode --state-dir ~/.claude --out project.csb
packed project.csb → claudecode
  sessions:    1
  memories:   1
  mcp servers: 1
  merkle root: 9542d49c31b322ed5eaa54e713f61a6dab6e8bc50270e96720baac75b612a95d
  fingerprint: 9542d49c31b3

$ carrystate verify --bundle project.csb
bundle:   project.csb
source:   claudecode
spec:     0.1
entries:  1 sessions, 1 memories, 1 mcp servers
merkle:   9542d49c31b322ed5eaa54e713f61a6dab6e8bc50270e96720baac75b612a95d
result:   4 matched, 0 mismatched
  ✓ all entries match — bundle is intact, round-trip is lossless
```

</details>

## 用法

<h2><img src="https://api.iconify.design/tabler/terminal-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 用法</h2>

**只迁一个项目**（大型 `~/.claude` 时按项目过滤）：

```bash
carrystate pack --harness claudecode --state-dir ~/.claude \
  --project -Users-me-work-myapp --out myapp.csb
```

**跨主机迁移**（信创内网典型路径，bundle 走 USB / 内部 artifact repo，从不出内网）：

```bash
# 源主机
carrystate pack --harness claudecode --state-dir ~/.claude --out project.csb
sha256sum project.csb >> 合规审计.log          # 记录 bundle 指纹

# 目标主机（USB / 内部 repo 拷贝 project.csb 过来）
carrystate verify --bundle project.csb         # 先验完整性，拒绝篡改 bundle
carrystate unpack --harness codex --in project.csb --state-dir ~/.codex
```

**干跑检查 bundle 内容**（不写盘）：

```bash
carrystate unpack --harness codex --in project.csb --state-dir ~/.codex --dry-run
```

完整 schema 与 entry 类型见 [SPEC.md](./SPEC.md)。更多示例见 [examples/happy-path.md](./examples/happy-path.md)。

## Demo

<h2><img src="https://api.iconify.design/tabler/photo.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> Demo</h2>

90 秒看到完整 round-trip：`pack → verify → unpack`，session 字节级无损。`docs/demo.tape` 是可复现的 [vhs](https://github.com/charmbracelet/vhs) 脚本，`.github/workflows/demo.yml` 在 CI 里重渲染。

![demo](assets/demo.gif)

## 对比 cc-switch

CarryState 与 [farion1231/cc-switch](https://github.com/farion1231/cc-switch) 互补而非竞争——它切 harness，CarryState 迁它留下的 state。诚实标注：cc-switch 在桌面 GUI 与受众规模上明显更强。

| 维度 | CarryState | cc-switch |
|---|:---:|:---:|
| 切换 harness | ✓（pack→unpack 迁 state） | ✓（切 launcher） |
| 迁移 sessions/memories/MCP | ✓ | ✗（每 switch 冷启动） |
| 跨 harness schema 翻译 | ✓（.csb 互换格式） | ✗ |
| 全程不出内网 | ✓ | ✓ |
| merkle 完整性校验 | ✓（合规审计可验证） | ✗ |
| 桌面 GUI | ✗（CLI only，v0.1） | ✓ |
| 受众规模 | 早期 | 124k 星 |

## 付费 / 企业版

<h2><img src="https://api.iconify.design/tabler/building-factory-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 付费 / 企业版</h2>

**OSS core 永远免费且功能完整**——v0.1 没有任何 paywall 功能。商业化路径是**同一 OSS 二进制**外包一层信创 on-prem 企业 license（¥50–200k/站/年），买的是合规与服务，不是功能门禁：

- **信创 SLA**：4 小时响应、对公合同、发票（fapiao）——信创企业不能用 Stripe/信用卡，v0.1 手动对公开票流足矣。
- **合规审计 log bundle**：每个 `.csb` 的 merkle root + manifest YAML 可直接进审计 trail，license 版提供打包的审计导出。
- **定制 harness adapter**：某个国产 harness 不在 v0.1 两个适配器内？license 含一次性定制开发。
- **现场 packing workshop**：design-partner 试点 90 天免费 → GA 转 ¥80k/站/年（单站 ≤50 席，SLA + audit）；含定制适配器至 ¥200k/站/年。

最小成交路径（此人群无信用卡）：周末 ops 访谈 → design-partner LOI（意向书）→ 90 天免费试点 + 现场 workshop → 试点末签对公合同 + 30% 预付款（~¥24k）。**License 交付**：同一 `carrystate` 二进制 + 离线 merkle-verifiable 签名 license 文件——契合 airgap。

联系：在 [Issues](https://github.com/SuperMarioYL/carrystate/issues) 开一个「企业 license 询价」标题的 issue 即可。

## 路线图

<h2><img src="https://api.iconify.design/tabler/map-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 路线图</h2>

- [x] **m1** — `carrystate pack --harness claudecode` 从真实 `~/.claude` 打出 merkle-校验的 `.csb`
- [x] **m2** — `carrystate unpack --harness codex` 写 Codex 原生布局 + `verify` 行为等价性通过
- [x] **m3** — `SPEC.md` + 双语 README + asciinema/vhs round-trip demo + `go install` 一键
- [ ] **v0.2** — OpenCode / Grok Build / Hermes Agent 适配器（bundle 格式已对称，加 adapter 即可）
- [ ] **v0.2** — 反向方向（Codex → Claude Code）
- [ ] **v0.2** — LLM-based memory merge / dedup / 语义对齐（v0.1 只做无损确定性翻译）
- [ ] **未来** — harness state dir 自动发现、社区 per-harness adapter 征集

## 贡献

<h2><img src="https://api.iconify.design/tabler/clipboard-list.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 贡献</h2>

Issue 与 PR 都欢迎。新增 harness adapter 请参考 `internal/harness/adapter.go` 的 `Adapter` 接口与 `SPEC.md` 的 schema 约定；bundle 格式对称，加 adapter 不需要改 spec。提交前 `go test ./...` 须通过。

## Share this

```
CarryState — 信创内网 agent state porter。把 Claude Code 的 sessions/memories/MCP 打包成 merkle-可校验的 .csb，导入 Codex 全程不出内网。Hoplite (YC S26) 把 state 迁往云；CarryState 留在本地。https://github.com/SuperMarioYL/carrystate
```

<p align="center"><sub><a href="./LICENSE">MIT</a> © 2026 SuperMarioYL</sub></p>
