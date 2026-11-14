# Blockchain-Anchored Multi-Strategy Portfolio Back-testing Framework

MSBD5017 课程项目：构建一个确定性的多策略投资组合回测系统，并用 SHA-256 Merkle root 和本地 EVM registry 对实验结果进行可验证锚定。

当前状态：**M0、M1 已完成，M2 尚未开始**。固定市场数据已经完成机械校验和规范化；尚未实现策略，也未使用 2025 final holdout 进行调参或绩效分析。

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

## 目录

```text
cmd/project/          CLI 入口
internal/doctor/      环境预检
internal/*            后续领域、执行、实验和证据模块
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
