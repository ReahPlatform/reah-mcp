# Reah MCP

让 AI 助手通过 MCP 读取和分析 Reah 财务数据：查看余额与账户、追踪交易和结算流水、
了解卡支出、发票和 Treasury 持仓。所有工具只读，并沿用 Reah 的用户权限。

提供 Go MCP 服务，以及 Claude Code、Codex 和 Cursor 的插件配置。
数据接口版本固定到 2026-10-18 的 Reah schema，来源与更新方式见文末。

## 运行

需要 Go 1.25.5+，以及能读取目标企业的 Reah 用户 access token。

```sh
make build
install -m 755 bin/reah-mcp /usr/local/bin/reah-mcp
export REAH_GRAPHQL_URL='https://YOUR-REAH-GRAPHQL-HOST/graphql'
export REAH_ENTITY_ID='YOUR_ENTITY_ID'
read -rs REAH_ACCESS_TOKEN
export REAH_ACCESS_TOKEN
reah-mcp
```

最后一条命令启动 stdio MCP，由客户端通过 stdin/stdout 通信。
服务日志只写 stderr。凭证通过进程环境提供，不写入插件配置或 Git。
token 过期后需要提供新 token 并重启服务；不复用浏览器 cookie、不自动续期。

通用 MCP 配置（先确保客户端进程继承上面的三个环境变量）：

```json
{"mcpServers":{"reah":{"command":"reah-mcp"}}}
```

桌面客户端若从 Finder 启动，通常不会继承终端中的 export。
请在客户端自己的 MCP 环境变量配置中提供这三个值，或从配置好环境的终端启动客户端。

## 插件安装

本仓库包含 Claude Code、Codex、Cursor / Agent Plugins 的 manifest 和
`reah-mcp` 查询技能。安装插件前先安装上面的二进制并配置客户端环境。
客户端需要 GitHub 私有仓库访问权限。

Claude Code：

```text
/plugin marketplace add ReahPlatform/reah-mcp
/plugin install reah@reah-marketplace
```

Codex：

```sh
codex plugin marketplace add ReahPlatform/reah-mcp
```

然后在 `/plugins` 中选择 Reah 安装。Cursor 可使用根目录的插件文件；
需要本地试用时，将仓库复制到 `~/.cursor/plugins/local/reah` 并重新加载窗口。
这些配置启动本地服务，没有预设尚未部署的域名。

## HTTP

```sh
export REAH_MCP_HTTP_TOKEN="$(openssl rand -hex 32)"
reah-mcp -transport http -listen 127.0.0.1:8080
```

Streamable HTTP 路径为 `/mcp`，所有 MCP 请求都需要
`Authorization: Bearer <REAH_MCP_HTTP_TOKEN>`。这是独立的 MCP 客户端访问凭证；
Reah access token 仅用于服务到 GraphQL 的调用。远程访问需要部署者提供 HTTPS 反向代理。
示例连接配置见 [examples/http.mcp.json](examples/http.mcp.json)。

一个服务实例固定使用一份用户凭证和一个企业 ID。拿到 HTTP token 的客户端
将以这份身份读取数据，因此不同用户/企业必须使用隔离实例。
当前使用部署者提供的访问凭证。托管 OAuth 登录需要另外配置 Reah OAuth 客户端、
授权 scope、回调及部署域名。

## 只读工具

| 工具 | 数据 |
| --- | --- |
| `reah_catalog` / `reah_describe_type` | 查询目录、企业 ID、字段、参数及单位说明 |
| `reah_get_entity` / `reah_list_balances` | 企业概览、按 instrument 分页的余额 |
| `reah_list_wallets` / `reah_list_banking_accounts` | 钱包、银行账户及余额 |
| `reah_list_transactions` / `reah_list_settled_movements` | 交易状态、结算流水和覆盖信息 |
| `reah_list_contacts` / `reah_list_invoices` | 联系人、发票 |
| `reah_list_approvals` / `reah_list_members` | 审批记录、企业成员 |
| `reah_list_card_accounts` / `reah_list_card_transactions` | 卡账户、脱敏卡信息、卡交易 |
| `reah_list_treasury_positions` / `reah_list_statements` | Treasury 持仓、已有报表任务 |
| `reah_list_assets` / `reah_search` | 资产目录、跨财务对象搜索 |
| `reah_query` | 允许范围内的自定义只读 GraphQL 查询 |

列表工具接受 `arguments`（GraphQL 参数）和可选 `fields`（结果内的 GraphQL
字段选择）。例如：

```json
{"arguments":{"first":100,"after":"PREVIOUS_CURSOR"},"fields":"transactions { id humanId type state } cursor"}
```

结算流水必须指定时间窗口：

```json
{"arguments":{"settledFrom":"2026-10-01T00:00:00Z","settledTo":"2026-11-01T00:00:00Z","first":100}}
```

继续结算流水分页时，把返回的 `nextPageToken` 传给 `arguments.after`。
其他 connection 一般返回 `cursor`；卡账户的 `cards.cursor` 是各账户独立的嵌套分页，
可以通过 `fields` 指定下一页的 `cards(first:100, after:"...")`。
单次读取不自动穷尽分页。搜索匹配也不等于完整财务数据集。

自定义查询中声明并使用 `$entityId: ID!`，服务自动填入配置的企业 ID：

```graphql
query Cash($entityId: ID!) {
  entity(id: $entityId) {
    id
    totalBalance { usdAmount priceVersion }
    treausryPositionAccountSummary { currentValueInUSD averageAPY }
  }
}
```

`treausry...` 拼写来自现有上游 API。先用 `reah_describe_type` 查询 `Query`、
`Entity` 或具体返回类型，再选择更详细的字段。

## 数据含义与边界

- 返回原始 GraphQL `data` 和 `errors`。保留十进制字符串、游标、结算覆盖及排除计数，
  不把缺失值转换成零。出现部分数据错误时，MCP 返回 `isError: true`，同时保留已返回的数据。
- 金额单位沿用上游字段：例如 `usdValue.amountMinor` 需按 `scale` 还原，
  `signedAmountSmallest` 需按 `instrumentDecimals` 还原。Treasury、卡限额和各种余额
  不应仅按字段名推断单位，先看字段说明。不要用浮点数处理金额。
- 交易列表包含不同生命周期状态；分析实际资金流应使用结算流水。
  原始交易不能直接相加替代 settled movement。
- 从 Agent 复用的是只读数据接口和固定 schema。没有复制 Agent 的归一化 helper、
  财务计算策略、spam-safe 余额归一化、证据封存、LLM、Temporal、浏览器或审批执行链。
  `totalBalance` 是上游原始观测，不等同于 Agent 的归一化 balance summary。
- MCP 在发出网络请求前解析并校验 GraphQL，只允许一个 query 和封闭的只读根字段，
  每个根字段的企业参数必须匹配配置。别名和 fragment 同样检查。
  schema 不包含 mutation、subscription、身份生命周期、Agent 对话和卡 PAN/CVV 入口。
  上游依然负责用户权限和具体资源的访问控制。
- query 最大 64 KiB、展开后最多 2000 个 selection、最多 16 层字段，显式分页参数
  限制为 1–200，响应最大 8 MiB，请求超时 30 秒。不会跟随重定向。
- 报表工具只读现有任务；创建报表或取得下载 URL 在上游属于 mutation，因此没有暴露。

## 开发与验证

```sh
make lint
make test
make build
```

测试覆盖所有业务工具的 MCP 调用、HTTP 协议/鉴权、非法查询在网络前被拒绝、
企业范围、分页、精度、部分错误、超限响应和重定向。
这些检查使用本地模拟 GraphQL；真实环境联调需要提供对应凭证。

[docs/upstream.json](docs/upstream.json) 记录 `reah-agent`
[`474fda4a`](https://github.com/ReahPlatform/reah-agent/commit/474fda4a771289782c5f9e793a4f0c33e7076f79)
提交及原 schema 文件的 SHA-256。
更新 pin 后执行 `make schema`，从相邻 `../reah-agent` 的指定 Git 提交重新提取
允许字段和可达类型；输入哈希不匹配会失败。原始写入 schema 不进入新仓库。
本项目不使用数据库、sqlc 或复制 sqlc 生成文件。
