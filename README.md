# Blockchain-Anchored Multi-Strategy Portfolio Back-testing Framework

MSBD5017 课程项目：构建一个确定性的多策略投资组合回测系统，并用 SHA-256 Merkle root 和本地 EVM registry 对实验结果进行可验证锚定。

当前状态：**M0-M9 全部完成**。2025 final holdout、canonical evidence、Merkle proofs、本地 EVM 锚定和 deterministic replay 均已通过冻结门禁；10/10 clean replay 字节一致，seed 5017 的 100/100 单字节 mutation 均被检测并定位。技术报告、五张可追溯图、D1-D3 清单和隔离 checkout rehearsal 已完成。

## V2：执行证据、多方审核与行情交叉验证

[`docs/V2_PROTOCOL.md`](docs/V2_PROTOCOL.md) 说明 v2 证据格式、角色签名、数据故障处理和信任边界。

- Go v2 bundle 复用已有 Merkle 编码，为 Agent/tool trace、完整交易账本、输入快照和质量报告生成逐文件 proof，支持局部文件验证。
- Agent 新增 `audit_market_data` 工具；缺失、损坏、配置偏离、来源不可用或价格分歧会阻止研究。
- Binance 与 Coinbase 的 21 个历史日线收盘价在声明的 200 bps 容差内比较；原始响应及 hash 入包，审核端离线重算。
- 发布者和审核者使用不同授权 Ed25519 密钥签署同一个根；审核签名前执行证据校验、行情重算及确定性重放。私钥不交给 Agent。
- 多源一致性提供有限佐证，不等于证明行情真实。v2 新证据不自动上链，v1 本地 EVM 证据保留。

## LangGraph Agent 框架

当前 Agent 框架位于 [`agents/portfolio_agent/`](agents/portfolio_agent/)。使用 LangGraph `StateGraph` 的 **agent → tools → agent** 循环：每轮模型根据目标、状态记忆和工具反馈决定工具名称与参数；条件边根据模型选择继续调用或结束。

```bash
make agent-bootstrap  # uv 创建 Python 3.12 环境并安装锁定依赖
make agent            # 新建目标会话，默认总预算 20 候选
make agent-test       # 框架的 checkpoint、失败恢复和结束门禁测试
```

主要组件：

- `runtime.py`：LangGraph 状态图、LangChain AIMessage/ToolMessage、工具注册调用、步数约束和逐步 trace。
- `provider.py`：可替换的 `ModelProvider` 适配器，当前通过 Codex app-server 返回结构化工具请求。
- `tools.py`：`prepare_environment`、`run_research`、`inspect_candidate`、`replay_research`、`write_report` 和 `audit_market_data` 六个带 schema 的工具；模型无法传入任意 shell 命令或输出路径。
- `cli.py`：SQLite checkpoint、会话创建、跨进程恢复、图结构导出和最终证据封装。

运行目录为 `artifacts/langgraph_*`，包含 `checkpoints.sqlite`、`events.jsonl`、每次工具调用的收据、实际 `graph.mmd` 和 `outputs/index.html`。模型可决定何时检查候选、何时重放、如何解释风险；工具执行错误会作为 observation 返回模型。结束门禁要求花完指定研究预算、重放成功并生成报告。策略计算仍由现有 Go 引擎完成，Agent 框架负责决策和调用工具。

```bash
PYTHONPATH=agents .venv-agent/bin/python -m portfolio_agent.cli --budget 6 --codex-bin /path/to/codex
PYTHONPATH=agents .venv-agent/bin/python -m portfolio_agent.cli --resume artifacts/langgraph_SESSION --codex-bin /path/to/codex
```

checkpoint 在图节点边界保存状态；已完成工具调用通过同一 call ID 的收据复用。内层搜索工具执行期间的中断可能需要重跑该工具，不承诺模型请求或单个回测内部任意位置恢复。`make agent-workflow` 现在指向 LangGraph 入口。

## 固定编排研究脚本（保留兼容）

```bash
make research-pipeline
```

旧版固定脚本依次完成：构建 Go 工具 → 检查固定数据（缺少时下载）→ Agent 研究计划 → 20 候选反馈搜索 → 执行选中策略并导出完整交易/账本 → 无模型确定性重放 → Agent 结果复盘 → 本次运行的网页可视化 → SHA-256 证据封装和验证。

输出目录为 `artifacts/workflow_*`，终端成功后打印 `WORKFLOW_COMPLETED` 和路径。打开其中的 `index.html` 可查看本次开发集净值曲线、候选对比和 Agent 复盘；`workflow.json` 记录各阶段状态，失败时保留诊断文件并以非零状态退出。`agent_plan.json`、`search_trace.jsonl`、`agent_review.json` 记录模型参与；`ensemble_trades.csv`、`ensemble_ledger.csv` 等是 Go 引擎执行的结果。Agent 负责研究决策，Go 工具负责受约束的计算和会计，不把模型的自然语言当作收益数据。

可以调整预算及模型，每次生成独立目录：

```bash
python3 scripts/agent_workflow.py --budget 6 --codex-bin /path/to/codex
python3 scripts/agent_workflow.py --verify artifacts/workflow_RUN_ID
./bin/project experiment agent-replay --bundle artifacts/workflow_RUN_ID
```

完整工作流默认使用已实测的 `gpt-6.1-sol`，可用 `--model` 或 `AGENT_MODEL` 覆盖，参数提议仍支持使用账号默认模型。预算限制为 2–20；baseline 固定，其余候选由模型根据已有开发集评价选择：每轮从 1,458 个有限空间候选中排除已评估项，再按固定顺序均匀抽取最多 32 个合法选项，模型返回选项索引与理由。选项在下一轮按新历史重新生成，避免重复提议消耗预算。模型失败默认终止，不静默变为手工搜索。重放从记录中的参数重新运行全部候选，逐字节核对四折结果、选中参数、信号、订单、成交、权重、收益与账本，模型调用为零。证据 manifest 检查所有文件及文件集合，检测改写、缺失和新增文件。

本扩展研究区间固定为 2021–2024，使用现有三类策略与动态组合的有限参数空间；它不生成任意策略源码。该区间已用于历史研究，因此新结果属于开发集实验，不能声称是新的盲测表现。新证据采用独立 `agent-workflow-1` SHA-256 文件 manifest，尚未上链；原 v1 的 2025 holdout 和链上 Merkle 证据仍属于原实验。

### 已完成的 20 候选运行

[`docs/agent_workflow_demo/index.html`](docs/agent_workflow_demo/index.html) 是一次完整成功运行的网页报告，目录同时发布所有 27 个证据文件及 manifest。模型为 `gpt-6.1-sol`，19 次参数选择调用，0 回退、0 拒绝，另外包含研究计划和复盘两次模型调用。全部四折指标与最终执行产物重放字节一致，重放没有模型调用。可直接验证公开副本：

```bash
python3 scripts/agent_workflow.py --verify docs/agent_workflow_demo
./bin/project experiment agent-replay --bundle docs/agent_workflow_demo
```

本次固定 baseline 的四折中位 Sharpe 为 0.667185、最差折最大回撤为 36.23%；选中候选为 1.637911、31.24%。选中参数为 trend 30/60，mean reversion 30/-1.5/-0.25，allocator 30/60。这是开发集搜索结果，存在选参偏差；报告保留全部候选和 Agent 对弱势折、成本与风险的分析。

## 可选扩展：AI 参数提议与交互式证据看板

`make dashboard` 会校验五张已提交 SVG 的 SHA-256，并生成可直接在浏览器打开的 [`docs/dashboard/index.html`](docs/dashboard/index.html)。页面可切换收益、回撤、权重、成本和市场状态图，同时展示冻结的 holdout 指标、Merkle root、重放及单字节变异验证记录。它是从报告生成的只读前端，不接交易所、不提交链上交易，也不重新计算或修改正式结果；因此无需常驻后端。

`make agent-search` 可在已执行 `make data` 的本地 checkout 上，运行一个 **development-only** 的 Codex app-server 参数提议实验。首个候选是固定 baseline；之后 Codex 只接收已评价候选的开发集聚合指标和参数，返回有限空间中的下一个候选。Go 引擎负责参数校验、四折回测、成本核算及冻结规则排名。输出仅写入被 Git 忽略的 `artifacts/development/agent_search_v2_*`，含 `search_trace.jsonl`、`fold_metrics.json` 和 `summary.json`；不会改写 `configs/selected_parameters.json`、2025 holdout、M6 Merkle root 或 M0-M9 报告。AI 搜索的结果属于补充实验，不能与已冻结的正式验证结果混称。

需要已登录的 Codex CLI（`codex login status`），也可以显式指定路径：

```bash
./bin/project experiment agent-search --root . --budget 3 --codex-bin /path/to/codex
```

预算为 1–20；默认 3。模型调用失败时默认报错，只有显式加 `--allow-manual-fallback` 才调用确定性手动提议器，并在 trace 和摘要中记录回退次数。完整运行还需要本地固定数据快照，克隆仓库后先执行 `make data`。

仓库附有一次真实的 3 候选试运行：[`docs/agent_pilot/summary.json`](docs/agent_pilot/summary.json)、[`search_trace.jsonl`](docs/agent_pilot/search_trace.jsonl) 和 [`fold_metrics.json`](docs/agent_pilot/fold_metrics.json)。Codex 提议了 2 个候选，0 次回退；该小样本中固定 baseline 仍胜出。trace 和四折指标的 SHA-256 写在摘要里，并由仪表盘构建脚本校验。这证明提议链路已实际执行，不证明 AI 提高了收益或发现了全局最优参数。

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

### 克隆后的可验证范围

直接运行 `make public-evidence` 可检查已提交报告中的参数身份、run ID、Merkle root、重放计数与交付清单中**实际存在**文件的 SHA-256。该检查只证明公开仓库内的报告和文件彼此一致，不能替代原始行情重建、Merkle proof 校验或链上交易复核。

`data/raw/`、`data/normalized/` 与 `artifacts/<run_id>/` 在 `.gitignore` 中，完整 568-leaf evidence bundle 不随 Git 仓库分发。若要独立运行 `make demo` 或 `make replay`，需先按 `make data` 获取并校验固定数据，再取得与报告 root 相符的完整 bundle；链上演示另需本地 Foundry/Anvil。提交到 chain ID 31337 的记录属于本地测试链，没有公开链的持续可查询性或最终性。对外展示时应明确这一限制。

```text
project doctor   检查工具可用性、版本和首次需要它的里程碑
project data build      下载、校验并规范化固定数据快照
project data download   只下载、校验 checksum 并解压
project data normalize  从本地 raw snapshot 重建规范化产物
project data validate   从本地 raw snapshot 重跑全部数据校验
project execution validate  运行 M2 手工会计场景并生成确定性报告
project strategies validate 运行 M3 三策略、无前视和串并行一致性验收
project experiment select 运行 M4 Walk-forward 搜索并冻结参数
project evidence build 构建 canonical bundle、Merkle root 和全部 proofs
project evidence verify 独立验证 bundle 和全部 proofs
project chain deploy 在本地 Anvil 部署 Registry 并运行 M7 链上验收
project replay <bundle> 执行 10 次完整重放、mutation、覆盖率与开销门禁
project mutate-test <bundle> 独立运行 seed 固定的单字节 mutation 测试
project help     显示帮助
project version  显示程序版本
```

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

冻结结果：

```text
run_id:     098376de7f855b9e
leaf_count: 568
root:       05136f84d6b1faa9226fd7b1a58b691707dc90be18688a2485fcbc3b7defa532
rebuilds:   10/10 identical
proofs:     568/568 valid
```

独立验证和单文件 proof：

```bash
project evidence verify --bundle artifacts/<run_id>
project evidence prove --bundle artifacts/<run_id> --path results/trades.csv
project evidence verify-proof --bundle artifacts/<run_id> --proof artifacts/<run_id>/merkle/proofs/<index>.json
```

Leaf 使用 `SHA256(0x00 || uint32_be(path_len) || NFC_path || uint64_be(content_len) || content)`；内部节点使用 `SHA256(0x01 || left || right)`。叶子按 UTF-8 路径字节排序，奇数节点复制最后一个节点，proof 显式记录 sibling 左右方向。

M6 报告：

- [`reports/m6_evidence_bundle.json`](reports/m6_evidence_bundle.json)：run ID、root、568 个 leaves 与 10 次重建结果；
- [`reports/m6_coverage_gap.json`](reports/m6_coverage_gap.json)：canonical/Merkle 覆盖率及 evidence orchestration 差距；
- `artifacts/098376de7f855b9e/`：完整 bundle、Merkle manifest 和 568 个 proofs，默认不提交。

## M7 Solidity Registry 与链上 proof

固定 Foundry v1.9.0 工具链并运行合约测试：

```bash
make foundry-bootstrap
make contract-test
make contract-coverage
```

正式本地链验收必须从 clean commit 启动：

```bash
make m7
```

该命令启动 chain ID 31337 的确定性 Anvil，部署 `BacktestRegistry`，提交 M6 root，然后用 `results/trades.csv` 的真实 10 层 proof 调用 `verifyStoredProof`。同时验证 zero/duplicate root rejection、event/storage 一致性、proof 长度门禁，以及 leaf、sibling、方向位三种篡改。

冻结结果：

```text
source commit:       6b2ad42e041bd60d932d77007a8963e4f263bc23
contract:            0x5fbdb2315678afecb367f032d93f642f64180aa3
root submission tx:  0x0ee983ca441892a46e784748d226553d4aaff20adf6cc44dacc92a1973324161
submission gas:      69,175
proof gas depth 1:   27,302
proof gas depth 5:   33,605
proof gas depth 10:  41,473
```

Foundry 5/5 测试通过，每个 fuzz test 固定运行 512 次；`BacktestRegistry.sol` 的 line、statement、branch 和 function coverage 均为 100%。链上产物写入 `artifacts/098376de7f855b9e/chain/`，按 SPEC 明确排除在 M6 Merkle tree 外，因此不会形成循环承诺，也不会改变已冻结 root。

主要证据：

- [`contracts/src/BacktestRegistry.sol`](contracts/src/BacktestRegistry.sol)：root registry 和有序 SHA-256 proof verifier；
- [`contracts/test/BacktestRegistry.t.sol`](contracts/test/BacktestRegistry.t.sol)：单元、负例和 fuzz tests；
- [`reports/m7_chain_validation.json`](reports/m7_chain_validation.json)：部署、提交、proof 和篡改门禁结果；
- [`reports/m7_contract_coverage.json`](reports/m7_contract_coverage.json)：合约本体覆盖率；
- `artifacts/098376de7f855b9e/chain/`：deployment、receipt、gas 和 proof verification 产物，默认不提交。

## M8 Replay、mutation 与质量保证

正式 M8 必须在 clean commit 上运行：

```bash
make m8
```

Replay 从 bundle 提取并校验冻结输入，在新的临时工作区重新运行三个基础策略、Ensemble、会计、指标和 19 个敏感性场景，然后重新生成 canonical results、deterministic event log、run manifest 和 Merkle tree。它不会下载数据，也不会调用代理。若不一致，verifier 会报告按路径排序的首个不同文件和首个 byte offset。

冻结结果：

```text
clean replay:              10/10 artifacts byte-identical
Merkle root:               10/10 identical
mutation seed/count:       5017 / 100
mutation detected:         100/100
mutated file localized:    100/100
network/agent calls:       0 / 0
core coverage minimum:     85.0%
average backtest:          1.164137 s
average evidence work:     0.083600 s
evidence overhead:         7.181285%
bundle storage total:      8,625,007 bytes
```

核心覆盖率门禁涵盖 canonical serialization、会计、Merkle、指标、组合、策略接口和三种策略实现；各包均不低于 85%。filesystem/report orchestration 由真实 bundle 的 10 次端到端 replay 和 100 次 mutation integration gate 覆盖。

主要证据：

- [`internal/replay/replay.go`](internal/replay/replay.go)：隔离式重放、byte diff、mutation 和 QA 编排；
- [`reports/m8_replay.json`](reports/m8_replay.json)：10 次逐次 root、字节一致性和耗时；
- [`reports/m8_mutation.json`](reports/m8_mutation.json)：100 次修改位置、原/新字节和定位结果；
- [`reports/m8_coverage.json`](reports/m8_coverage.json)：逐包核心覆盖率；
- [`reports/m8_overhead.json`](reports/m8_overhead.json)：回测、证据开销及 bundle 存储分解。

## M9 最终报告与演示

重新生成图表和交付清单：

```bash
make report
make deliverables
```

运行完整演示：

```bash
make demo
```

`make demo` 会验证固定数据、展示 final holdout、验证全部 568 个 leaves/proofs、验证真实 trades proof、启动全新 Anvil 完成部署和链上 proof，并运行 100 次 mutation。Rehearsal 使用临时 detached checkout 执行，tracked source 在运行后保持 clean。

最终文件：

- [`docs/TECHNICAL_REPORT.md`](docs/TECHNICAL_REPORT.md)：研究问题、方法、结果、验证和局限；
- [`docs/DEMO.md`](docs/DEMO.md)：九步端到端演示说明；
- [`docs/figures/`](docs/figures/)：equity、drawdown、weights、cost drag 和 regime 图；
- [`reports/m9_chart_manifest.json`](reports/m9_chart_manifest.json)：每张图及其 bundle 数据源的 SHA-256；
- [`deliverables/manifest.json`](deliverables/manifest.json)：D1、D2、D3 代表文件的 byte length 和 SHA-256；
- [`reports/m9_rehearsal.json`](reports/m9_rehearsal.json)：隔离 checkout 最终演示门禁。

报告明确限制结论：区块链只证明特定 bytes 的承诺、提交者和顺序，不证明数据真实，也不证明策略未来盈利。

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
