# Blockchain-Anchored Multi-Strategy Portfolio Back-testing Framework

MSBD5017 课程项目：构建一个确定性的多策略投资组合回测系统，并用 SHA-256 Merkle root 和本地 EVM registry 对实验结果进行可验证锚定。

当前状态：**M0-M5 已完成，M6 canonical evidence 与 Merkle 构建器已就绪**。2025 final holdout 已按冻结参数正式运行一次，结果没有反馈到参数选择；M6 bundle 尚未正式生成。

## 规格与计划

- [`SPEC.md`](SPEC.md)：冻结的实验、时间语义、指标和证据格式规范；
- [`PLAN.md`](PLAN.md)：M0-M9 实施顺序、验收门槛和风险控制。

实现不得静默偏离 `SPEC.md`。如果需要修改资产、日期、成本、时间语义、指标或 Merkle 规则，应先提升 SPEC 版本。

## M0 快速开始

项目要求 Go 1.24 或更高版本。仓库提供经过 SHA-256 校验的本地 Go 工具链安装脚本，不修改系统目录：

```bash
make bootstrap
make doctor
make ci
```

也可以分别执行：

```bash
make build
./bin/project doctor
make test
```

`make doctor` 中：

- Git 和 Go 是 M0 必需工具；
- Foundry/Anvil 到 M7 才是必需工具；
- Node/Codex 到 M4 的代理参数提议阶段才会使用，并且存在 manual adapter 回退方案。

因此，后续阶段工具显示 `MISSING` 不代表 M0 失败。`make ci` 才是当前里程碑的完整质量门禁。

## 当前 CLI

```text
project doctor   检查工具可用性、版本和首次需要它的里程碑
project data build      下载、校验并规范化固定数据快照
project data download   只下载、校验 checksum 并解压
project data normalize  从本地 raw snapshot 重建规范化产物
project data validate   从本地 raw snapshot 重跑全部数据校验
project execution validate  运行 M2 手工会计场景并生成确定性报告
project strategies validate 运行 M3 三策略、无前视和串并行一致性验收
project experiment select 运行 M4 Walk-forward 搜索并冻结参数
project help     显示帮助
project version  显示程序版本
```

其余策略、证据和链上命令将在对应里程碑中实现，当前不会放置返回假成功的占位命令。

## M1 固定数据快照

完整构建命令：

```bash
make data
```

只使用已经下载的 raw snapshot 重放校验：

```bash
make data-validate
```

输出包括：

- `data/raw/`：180 个 ZIP、180 个官方 CHECKSUM 和 180 个解压 CSV，默认不提交；
- `data/normalized/daily_ohlcv.csv`：按 `date ASC, symbol ASC` 排序的 5,478 行日线，默认不提交；
- [`data/data_manifest.json`](data/data_manifest.json)：URL、checksum、压缩与解压文件 hash、行数、时间戳单位；
- [`data/validation_report.json`](data/validation_report.json)：日期连续性、重复值、OHLC 和时间戳边界结果；
- [`data/LICENSE_DATA.md`](data/LICENSE_DATA.md)：Binance Vision 数据署名与使用限制。

数据管道会机械处理 2025 年微秒时间戳，但在 M5 门禁之前，任何策略、代理或参数选择代码都不得读取 2025 数据。

## M2 执行会计核心

运行完整 M2 验收：

```bash
make execution
```

会计核心独立于策略，支持纯现金、买入、卖出、跨资产调仓、统一现金缩单和全清仓。执行顺序固定为卖单优先，同方向按 `symbol ASC`；成交数量限制为 8 位小数，低于 10 USDT 的订单会被确定性跳过。

核心文件：

- [`configs/costs.json`](configs/costs.json)：冻结的资金、成本、精度和容差；
- [`internal/domain/types.go`](internal/domain/types.go)：MarketBar、Signal、TargetWeight、Order、Trade、Position 和 LedgerEntry；
- [`internal/execution/engine.go`](internal/execution/engine.go)：成本、现金、持仓、PnL 和调仓逻辑；
- [`tests/golden/execution_ledger.csv`](tests/golden/execution_ledger.csv)：五日手工场景 golden ledger；
- [`reports/m2_execution_validation.json`](reports/m2_execution_validation.json)：M2 确定性验收报告。

每日同时验证：

```text
equity = cash + Σ position_value
net_pnl = gross_pnl - commission - slippage
```

两个归一化残差都必须小于 `1e-10`。

## M3 三种基础策略

运行完整 M3 验收：

```bash
make strategies
```

该命令先生成三个 portable WASI 模块，再以串行和并行两种模式运行 Buy-and-hold、SMA trend（20/100）和有状态 z-score mean reversion（20/-1/0）。统一时间语义是：日期 `d` 收盘后生成信号，日期 `d+1` 收盘执行，随后才承担持仓收益。

核心文件：

- [`configs/strategies.json`](configs/strategies.json)：development 边界、资产和基准参数；
- [`configs/argo.json`](configs/argo.json)：Argo 来源版本及隔离式 adapter 决策；
- [`internal/strategy/strategy.go`](internal/strategy/strategy.go)：只暴露历史区间查询的策略宿主边界；
- [`internal/experiment/runner.go`](internal/experiment/runner.go)：滞后执行、串并行运行及稳定合并；
- [`reports/m3_strategy_validation.json`](reports/m3_strategy_validation.json)：产物和 WASM hash、无前视及一致性结果。

完整 CSV 证据写入 `artifacts/development/base_strategies/`，包含 signals、target/executed weights、orders、trades、returns 和 ledger，默认不提交。策略数据加载器在进入策略前即截断 2025 数据。

Argo 当前 `main`（记录于 `configs/argo.json`）要求 Go 1.26.2，项目 module baseline 是 Go 1.24.0，固定构建工具链是 Go 1.27.4，因此工具链兼容。M3 仍使用与 Argo `GetRange` 对应的 adapter boundary 隔离策略数学，并生成 portable WASI 核心模块；验收结果只以本项目冻结的执行会计引擎为准，不受 Argo host 自身成交语义影响。真正的 Argo host registration wrapper 不属于本次 M3 验收产物，后续可在不改变策略或回测语义的前提下接入。

## M4 Walk-forward 参数选择与 Ensemble

运行冻结的 20 候选选择流程：

```bash
make m4
```

四个 validation fold、参数空间和选择规则均来自 `SPEC.md`。第一个候选固定为 baseline；其余候选由 `AgentProposer` 的 `manual_deterministic` fallback 根据当前 validation leader 进行确定性空间探索。该过程不调用模型，完整 trace 可重复 hash，也不声称遍历了 1,458 个联合候选或找到了全局最优解。

冻结结果：

```text
SMA trend:          fast=10, slow=150
Mean reversion:     lookback=10, entry_z=-0.5, exit_z=-0.25
Ensemble allocator: reliability_window=30, volatility_window=30
Validation median net Sharpe: 1.412998347587
```

主要证据：

- [`configs/selected_parameters.json`](configs/selected_parameters.json)：冻结参数、选择指标和 search trace hash；
- [`configs/schemas/parameters.schema.json`](configs/schemas/parameters.schema.json)：有限参数空间 schema；
- [`reports/m4_parameter_selection.json`](reports/m4_parameter_selection.json)：20 候选、四折、缓存和产物 hash；
- [`reports/m4_coverage_gap.json`](reports/m4_coverage_gap.json)：核心覆盖率与 orchestration 差距清单；
- `artifacts/development/walk_forward/`：search trace、fold metrics、development metrics、Ensemble 权重和完整账本，默认不提交。

Ensemble 日期 `d` 的 reliability 与 volatility 只读取 `d-1` 及以前的基础策略净收益，日期 `d` 形成的资产目标在 `d+1` 收盘执行。`selected_parameters.json` 明确记录 `final_holdout_opened=false`；M5 之前不得读取这些 development 结果重新改变参数。

## M5 Final holdout 门禁

M5 分为两个明确阶段。首先在 clean commit 上只读取 development 数据，冻结市场状态波动率阈值：

```bash
make m5-freeze
```

提交生成的 `configs/final_experiment.json` 并再次确认工作区 clean 后，才可执行一次正式 2025 holdout：

```bash
make m5
```

正式运行已从 clean commit `b075cdd` 完成，命令在已有 M5 报告时拒绝覆盖。它运行三个基础策略和滞后 Ensemble，输出总体指标、四种滞后市场状态指标，以及 baseline、参数和成本 one-factor-at-a-time ±20% 共 19 个场景。敏感性结果仅用于解释，不能反馈到参数选择。

Ensemble 的 2025 结果为：年化收益 `0.048342760182`、Sharpe `0.328692969052`、最大回撤 `0.248998005359`。四种状态分别包含 Bull/LowVol 157 日、Bull/HighVol 40 日、Bear/LowVol 104 日、Bear/HighVol 64 日。

主要证据：

- [`configs/final_experiment.json`](configs/final_experiment.json)：holdout 前冻结的阈值、输入 hash 和参数身份；
- [`reports/m5_final_holdout.json`](reports/m5_final_holdout.json)：四种策略的 2025 总体指标和 artifact hashes；
- [`reports/m5_regime_metrics.json`](reports/m5_regime_metrics.json)：四种滞后市场状态指标；
- [`reports/m5_sensitivity.json`](reports/m5_sensitivity.json)：19 个 OFAT 场景及相对 baseline 变化；
- [`reports/m5_run_log.json`](reports/m5_run_log.json)：正式运行序号、clean-tree 声明和源码 commit；
- `artifacts/final_holdout/`：gross/net returns、signals、weights、orders、trades、ledger 和状态标签，默认不提交。

## M6 Canonical evidence 与 Merkle

在 clean commit 上执行正式 M6 构建：

```bash
make m6
```

命令会从固定输入完整重建 10 次 leaf set 和 Merkle root，然后写入 `artifacts/<run_id>/`。原始数据、规范、配置、development search trace、manifests、final results、确定性事件日志和 `run_manifest.json` 全部进入树；`merkle/` proofs、manifest 及未来 `chain/` 文件明确排除，避免循环依赖。

独立验证和单文件 proof：

```bash
project evidence verify --bundle artifacts/<run_id>
project evidence prove --bundle artifacts/<run_id> --path results/trades.csv
project evidence verify-proof --bundle artifacts/<run_id> --proof artifacts/<run_id>/merkle/proofs/<index>.json
```

Leaf 使用 `SHA256(0x00 || uint32_be(path_len) || NFC_path || uint64_be(content_len) || content)`；内部节点使用 `SHA256(0x01 || left || right)`。叶子按 UTF-8 路径字节排序，奇数节点复制最后一个节点，proof 显式记录 sibling 左右方向。

## 目录

```text
cmd/project/          CLI 入口
internal/doctor/      环境预检
internal/*            领域、执行、策略运行、实验和证据模块
strategies/           三种策略插件
contracts/            Solidity、测试和部署脚本
configs/              固定实验配置和 schema
data/                 数据许可、raw 与 normalized 数据
artifacts/            生成的实验产物（默认不提交）
tests/                 fixtures 与 golden files
reports/               技术报告材料
```

## M0 验证

```bash
make ci
git status --short
git tag --list spec-v1.0.0
```

预期结果：测试和构建成功、Git 工作区为空，并且 `spec-v1.0.0` 标签存在。
